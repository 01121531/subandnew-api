import { describe, expect, test } from 'bun:test'

import { mailboxAssistantDownloadUrl } from './mailbox-assistant-download'

describe('mailbox assistant release download', () => {
  test('matches the running release and downloads the client, not the server', () => {
    expect(mailboxAssistantDownloadUrl('v1.2.86')).toBe(
      'https://github.com/01121531/subandnew-api/releases/download/v1.2.86/mailbox-assistant-v1.2.86-windows-amd64.zip'
    )
  })
  test('development, prerelease and unsafe versions use the release page', () => {
    for (const value of [
      undefined,
      null,
      '',
      'dev',
      'v1.2.86-rc1',
      '../x',
      'https://evil.test',
      'v1.2.86?token=secret',
    ]) {
      expect(mailboxAssistantDownloadUrl(value)).toBe(
        'https://github.com/01121531/subandnew-api/releases/latest'
      )
    }
  })
})
