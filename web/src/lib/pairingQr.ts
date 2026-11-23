import qrcode from 'qrcode-generator';

/** A QR code as a single SVG path over a square grid of `size` modules (quiet zone included). */
export interface QrMatrix {
  size: number;
  path: string;
}

/** The quiet zone the QR specification asks for around the code, in modules. */
const QUIET_ZONE = 4;

/** Draws `text` as a QR code; the smallest version that holds it is chosen, with a medium error correction. */
export function qrMatrix(text: string): QrMatrix {
  const code = qrcode(0, 'M');
  code.addData(text);
  code.make();
  const count = code.getModuleCount();
  let path = '';
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (code.isDark(row, col)) path += `M${col + QUIET_ZONE} ${row + QUIET_ZONE}h1v1h-1z`;
    }
  }
  return { size: count + 2 * QUIET_ZONE, path };
}
