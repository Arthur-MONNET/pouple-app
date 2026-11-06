<script setup lang="ts">
import done from '../assets/mascot/mascot-done.svg';
import doneDark from '../assets/mascot/mascot-done-dark.svg';
import investigate from '../assets/mascot/mascot-investigate.svg';
import investigateDark from '../assets/mascot/mascot-investigate-dark.svg';
import multitask from '../assets/mascot/mascot-multitask.svg';
import multitaskDark from '../assets/mascot/mascot-multitask-dark.svg';
import oops from '../assets/mascot/mascot-oops.svg';
import oopsDark from '../assets/mascot/mascot-oops-dark.svg';
import sleep from '../assets/mascot/mascot-sleep.svg';
import sleepDark from '../assets/mascot/mascot-sleep-dark.svg';
import stop from '../assets/mascot/mascot-stop.svg';
import stopDark from '../assets/mascot/mascot-stop-dark.svg';

export type MascotPose = 'multitask' | 'investigate' | 'done' | 'sleep' | 'stop' | 'oops';
/** `lavender` is the navy-outline art for the lavender stage; `dark` has a lavender outline that shows on dark. */
export type MascotSurface = 'lavender' | 'dark';

const SOURCES: Record<MascotSurface, Record<MascotPose, string>> = {
  lavender: { multitask, investigate, done, sleep, stop, oops },
  dark: { multitask: multitaskDark, investigate: investigateDark, done: doneDark, sleep: sleepDark, stop: stopDark, oops: oopsDark },
};

/** The octopus is decoration: it never carries information, so it has no alternative text. */
withDefaults(defineProps<{ pose: MascotPose; surface?: MascotSurface }>(), { surface: 'dark' });
</script>

<template>
  <img :src="SOURCES[surface][pose]" alt="" draggable="false" class="select-none object-contain" />
</template>
