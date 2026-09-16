export function pastedImages(
  data: Pick<DataTransfer, 'items' | 'files'>
): File[] {
  const files = [...data.items]
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter((file): file is File => file !== null)
  return files.length
    ? files
    : [...data.files].filter((file) => file.type.startsWith('image/'))
}

export async function readClipboardImages(
  read: () => Promise<ClipboardItem[]>
): Promise<File[]> {
  const items = await read()
  const files: File[] = []
  for (const item of items) {
    // One image representation per clipboard item; never fetch HTML image URLs.
    const type =
      ['image/png', 'image/jpeg', 'image/webp'].find((value) =>
        item.types.includes(value)
      ) ?? item.types.find((value) => value.startsWith('image/'))
    if (!type) continue
    const blob = await item.getType(type)
    files.push(new File([blob], `clipboard-${files.length + 1}`, { type }))
  }
  return files
}
