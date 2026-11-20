import { bearer, toApiError } from './client';

export interface TerminalState {
  id: string;
  user: boolean;
  busy: boolean;
  waiting: boolean;
  shellExited: boolean;
  revision: number;
  output: string | null;
  offset: number;
  commands: string;
  commandOffset: number;
}

export async function terminalRequest(requestId: string, payload: object, signal: AbortSignal): Promise<TerminalState> {
  const response = await fetch(`/api/assistant/requests/${encodeURIComponent(requestId)}/terminal`, {
    method: 'POST',
    headers: { ...bearer(), 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal,
  });
  if (!response.ok) throw await toApiError(response);
  return (await response.json()) as TerminalState;
}
