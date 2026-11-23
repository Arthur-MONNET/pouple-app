import { computed, onScopeDispose, ref } from 'vue';
import { errorMessage, type PoupleApi } from '../api/client';
import type { AddressOffer, PairingOffer, RemoteDevice, RemoteSettings, RemoteSettingsPatch } from '../api/types';
import { defaultInterfaces, externalEndpoint, freshDevices, secondsLeft } from '../lib/phonePairing';

/** How often the clock of the countdown ticks, and how often the device list is asked while a window is open. */
export const TICK_MS = 1000;
export const DEVICE_POLL_MS = 3000;

/** `open`: waiting for a phone. `expired`: the window closed by itself. `paired`: a new phone showed up in the list. */
export type PairingPhase = 'idle' | 'opening' | 'open' | 'expired' | 'paired';

/**
 * The phone access screen: the settings, the paired phones and the pairing window.
 *
 * Every refresh carries a ticket so a slow answer never overwrites a newer one, every action is refused while
 * another of its kind runs (a double click sends nothing), and the timers stop with the scope that owns them.
 */
export function usePhoneAccess(api: PoupleApi, now: () => number = Date.now) {
  const settings = ref<RemoteSettings | null>(null);
  const devices = ref<RemoteDevice[] | null>(null);
  const loading = ref(true);
  const loadError = ref<string | null>(null);
  const devicesError = ref<string | null>(null);

  const saving = ref(false);
  const saveError = ref<string | null>(null);

  const revoking = ref<ReadonlySet<string>>(new Set());
  const revokeError = ref<string | null>(null);

  const phase = ref<PairingPhase>('idle');
  const offer = ref<PairingOffer | null>(null);
  const pairingError = ref<string | null>(null);
  const clock = ref(now());

  const address = ref<AddressOffer | null>(null);
  const addressLoading = ref(false);
  const addressError = ref<string | null>(null);
  const pairedName = ref<string | null>(null);

  const remaining = computed(() => (offer.value ? secondsLeft(offer.value.expiresAt, clock.value) : 0));
  /** Pairing needs the access on and a listener that really runs. */
  const canPair = computed(() => settings.value?.enabled === true && (settings.value?.listening.length ?? 0) > 0 && !settings.value.problem);

  /** The address update needs the access from outside on and an address the core knows (a name, or one it detected). */
  const canShareAddress = computed(() => {
    const current = settings.value;
    if (!current || !current.enabled || !current.externalEnabled || current.listening.length === 0 || current.problem) return false;
    return externalEndpoint(current) !== null;
  });

  let disposed = false;
  let addressTicket = 0;
  let tick: ReturnType<typeof setInterval> | undefined;
  let poll: ReturnType<typeof setInterval> | undefined;
  let knownDevices = new Set<string>();
  let settingsTicket = 0;
  let devicesTicket = 0;
  let pairingTicket = 0;

  function stopTimers(): void {
    clearInterval(tick);
    clearInterval(poll);
    tick = poll = undefined;
  }

  async function refreshSettings(): Promise<void> {
    const ticket = ++settingsTicket;
    try {
      const next = await api.getRemoteSettings();
      if (disposed || ticket !== settingsTicket) return;
      settings.value = next;
      loadError.value = null;
    } catch (cause) {
      if (disposed || ticket !== settingsTicket) return;
      loadError.value = errorMessage(cause);
    }
  }

  /** Returns the list it read, or null when the answer was stale or failed. */
  async function refreshDevices(): Promise<RemoteDevice[] | null> {
    const ticket = ++devicesTicket;
    try {
      const next = await api.listRemoteDevices();
      if (disposed || ticket !== devicesTicket) return null;
      devices.value = next;
      devicesError.value = null;
      return next;
    } catch (cause) {
      if (disposed || ticket !== devicesTicket) return null;
      devicesError.value = errorMessage(cause);
      return null;
    }
  }

  async function load(): Promise<void> {
    loading.value = true;
    await Promise.all([refreshSettings(), refreshDevices()]);
    if (!disposed) loading.value = false;
  }

  function endPairing(next: PairingPhase): void {
    stopTimers();
    pairingTicket++;
    phase.value = next;
  }

  function onTick(): void {
    clock.value = now();
    if (phase.value === 'open' && remaining.value === 0) {
      endPairing('expired');
      // The phone may have paired in the last seconds: look once more before saying nothing happened.
      void checkPaired();
    }
  }

  async function checkPaired(): Promise<void> {
    const list = await refreshDevices();
    if (!list || disposed) return;
    const [arrived] = freshDevices(knownDevices, list);
    if (arrived && (phase.value === 'open' || phase.value === 'expired')) {
      pairedName.value = arrived.name;
      endPairing('paired');
    }
  }

  async function startPairing(): Promise<void> {
    if (phase.value === 'opening' || phase.value === 'open' || !canPair.value) return;
    stopTimers();
    const ticket = ++pairingTicket;
    phase.value = 'opening';
    pairingError.value = null;
    pairedName.value = null;
    // A phone is "new" when it was not in the list the moment the window opened.
    const before = await refreshDevices();
    knownDevices = new Set((before ?? devices.value ?? []).map((device) => device.id));
    if (disposed || ticket !== pairingTicket) return;
    try {
      const opened = await api.openPairing();
      if (disposed || ticket !== pairingTicket) {
        // Cancelled while it was opening: do not leave a window open on the Mac.
        if (!disposed) void api.cancelPairing().catch(() => undefined);
        return;
      }
      offer.value = opened;
      clock.value = now();
      phase.value = remaining.value === 0 ? 'expired' : 'open';
      if (phase.value === 'open') {
        tick = setInterval(onTick, TICK_MS);
        poll = setInterval(() => void checkPaired(), DEVICE_POLL_MS);
      }
    } catch (cause) {
      if (disposed || ticket !== pairingTicket) return;
      pairingError.value = errorMessage(cause);
      offer.value = null;
      phase.value = 'idle';
    }
  }

  async function cancelPairing(): Promise<void> {
    const wasOpen = phase.value === 'open' || phase.value === 'opening';
    endPairing('idle');
    offer.value = null;
    pairedName.value = null;
    if (!wasOpen) return;
    try {
      await api.cancelPairing();
    } catch (cause) {
      if (!disposed) pairingError.value = errorMessage(cause);
    }
  }

  /** Leaves the finished state (expired or paired) to go back to the button. */
  function dismissPairing(): void {
    if (phase.value === 'expired' || phase.value === 'paired') {
      endPairing('idle');
      offer.value = null;
      pairedName.value = null;
    }
  }

  async function save(patch: RemoteSettingsPatch): Promise<boolean> {
    if (saving.value) return false;
    saving.value = true;
    saveError.value = null;
    try {
      const patched: RemoteSettingsPatch = { ...patch };
      // The core refuses to turn the access on with no network chosen: pick the usual one for the person.
      if (patch.enabled === true && patch.interfaces === undefined && (settings.value?.interfaces.length ?? 0) === 0) {
        const chosen = defaultInterfaces(settings.value?.available ?? []);
        if (chosen.length > 0) patched.interfaces = chosen;
      }
      // Answers of an older refresh must not come back over what was just saved.
      settingsTicket++;
      const next = await api.updateRemoteSettings(patched);
      if (disposed) return true;
      settings.value = next;
      // What the address says may have changed with the settings: it is asked again on demand.
      if (address.value || addressError.value) hideAddress();
      // The listener restarted: its window is gone.
      if (phase.value === 'open' || phase.value === 'opening') {
        endPairing('idle');
        offer.value = null;
      }
      return true;
    } catch (cause) {
      if (!disposed) saveError.value = errorMessage(cause);
      return false;
    } finally {
      if (!disposed) saving.value = false;
    }
  }

  /** Asks the core for the address update payload (no secret, no pairing window). */
  async function showAddress(): Promise<void> {
    if (addressLoading.value) return;
    const ticket = ++addressTicket;
    addressLoading.value = true;
    addressError.value = null;
    try {
      const next = await api.getRemoteAddress();
      if (disposed || ticket !== addressTicket) return;
      address.value = next;
    } catch (cause) {
      if (disposed || ticket !== addressTicket) return;
      address.value = null;
      addressError.value = errorMessage(cause);
    } finally {
      if (!disposed && ticket === addressTicket) addressLoading.value = false;
    }
  }

  function hideAddress(): void {
    addressTicket++;
    address.value = null;
    addressError.value = null;
    addressLoading.value = false;
  }

  async function revoke(id: string): Promise<boolean> {
    if (revoking.value.has(id)) return false;
    revoking.value = new Set(revoking.value).add(id);
    revokeError.value = null;
    try {
      await api.revokeRemoteDevice(id);
      await refreshDevices();
      return true;
    } catch (cause) {
      if (!disposed) revokeError.value = errorMessage(cause);
      return false;
    } finally {
      if (!disposed) {
        const rest = new Set(revoking.value);
        rest.delete(id);
        revoking.value = rest;
      }
    }
  }

  onScopeDispose(() => {
    disposed = true;
    stopTimers();
    // A window left open on the Mac for nobody would stay valid up to 2 minutes.
    if (phase.value === 'open' || phase.value === 'opening') void api.cancelPairing().catch(() => undefined);
  });

  return {
    settings,
    devices,
    loading,
    loadError,
    devicesError,
    saving,
    saveError,
    revoking,
    revokeError,
    phase,
    offer,
    remaining,
    pairingError,
    pairedName,
    canPair,
    address,
    addressLoading,
    addressError,
    canShareAddress,
    showAddress,
    hideAddress,
    load,
    refreshDevices,
    save,
    startPairing,
    cancelPairing,
    dismissPairing,
    revoke,
  };
}

export type PhoneAccess = ReturnType<typeof usePhoneAccess>;
