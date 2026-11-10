import { expect, test } from 'nsh/test'
import { INSTALL_COMMAND } from './update.tsx'

// The phone shows this to a Mac it can't update. An Intel Mac can't run the Apple silicon file, so the command picks
// the file by the Mac's processor, as INSTALL.md's Mac step does.
test('the install-by-hand command picks the Mac file by processor', () => {
  expect(INSTALL_COMMAND).toContain("releases/latest/download/herdr-anywhere-macos-$(uname -m | sed 's/x86_64/amd64/')")
  expect(INSTALL_COMMAND).not.toContain('herdr-anywhere-macos-arm64')
})
