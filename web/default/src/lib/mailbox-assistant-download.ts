const releases = 'https://github.com/01121531/subandnew-api/releases'

export function mailboxAssistantDownloadUrl(version: unknown): string {
  if (typeof version !== 'string' || !/^v\d+\.\d+\.\d+$/.test(version)) {
    return `${releases}/latest`
  }
  return `${releases}/download/${version}/mailbox-assistant-${version}-windows-amd64.zip`
}
