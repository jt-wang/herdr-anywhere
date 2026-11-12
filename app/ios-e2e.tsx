// The app e2e/ios.sh installs: the real app, opened at the run's pairing link (a script cannot tap the simulator;
// nsh.json must sit next to the entry, so it is here). For the same reason it opens on Settings on the launch
// e2e/open-settings.ts names, which e2e/ios-tailcat.sh sets.
import { Storage } from 'nsh'
import { start } from './src/main.tsx'
import { OPEN_SETTINGS_ON_LAUNCH } from './e2e/open-settings.ts'
import { PAIR } from './e2e/pair.ts'

void Storage.getItem('e2e-launches').then((n) => {
  const launch = Number(n ?? 0) + 1
  void Storage.setItem('e2e-launches', String(launch))
  start({ pairLink: PAIR, opensSettings: launch === OPEN_SETTINGS_ON_LAUNCH })
})
