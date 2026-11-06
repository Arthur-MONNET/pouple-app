/** Recursively applies `fn` to every string of a message tree. */
export type MessageTree = { [key: string]: string | MessageTree };

export function mapMessages(tree: MessageTree, fn: (text: string) => string): MessageTree {
  return Object.fromEntries(Object.entries(tree).map(([key, value]) => [key, typeof value === 'string' ? fn(value) : mapMessages(value, fn)]));
}

const NBSP = ' ';
const NARROW_NBSP = ' ';

/**
 * French typography, applied once to the French catalog so the source can be written with plain spaces:
 * no-break space before ":" and inside guillemets, narrow no-break space before "; ! ?", and the curly apostrophe.
 */
export function frenchTypography(text: string): string {
  return text
    .replace(/(\p{L})'(\p{L})/gu, '$1’$2')
    .replace(/ ([;!?])/g, `${NARROW_NBSP}$1`)
    .replace(/ :/g, `${NBSP}:`)
    .replace(/« /g, `«${NBSP}`)
    .replace(/ »/g, `${NBSP}»`);
}
