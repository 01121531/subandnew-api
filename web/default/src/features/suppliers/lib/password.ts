export function generateSupplierPassword(): string {
  const alphabet =
    'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
  const bytes = new Uint8Array(24)
  globalThis.crypto.getRandomValues(bytes)
  // A power-of-two alphabet keeps each generated symbol uniformly distributed.
  return Array.from(bytes, (byte) => alphabet[byte & 63]).join('')
}
