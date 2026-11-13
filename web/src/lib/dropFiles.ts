/** Whether a drag carries files (and not a selected text or a link): only those are for the conversation. */
export function hasFiles(event: DragEvent): boolean {
  return event.dataTransfer?.types.includes('Files') ?? false;
}
