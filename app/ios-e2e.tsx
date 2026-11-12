// The app e2e/ios.sh installs: the real app, opened at the run's pairing link (a script cannot tap the simulator;
// nsh.json must sit next to the entry, so it is here). For the same reason it opens on Settings on the launch
// e2e/open-settings.ts names.
// A script can also hand both over after the build, in the app's storage (storage.json in its data container, written
// before the launch): e2e-pair and e2e-open-settings, read here first. e2e/ios-tailcat.sh does that, since a Tailcat
// invite lasts 2 minutes and a build after it opened took longer on CI.
// iOS asks about notifications in a system alert over the app the first time it connects, which a script cannot
// answer and which would cover every screenshot: it counts as asked already, unless e2e-ask-notifications is "1"
// (e2e/ios-update-notify.sh answers it with XCUITest). e2e-update-every: how often, in ms, the app reads the
// computer's answer about new versions (an hour otherwise).
import { Storage } from 'nsh'
import { start } from './src/main.tsx'
import { OPEN_SETTINGS_ON_LAUNCH } from './e2e/open-settings.ts'
import { PAIR } from './e2e/pair.ts'

void Promise.all([Storage.getItem('e2e-launches'), Storage.getItem('e2e-pair'), Storage.getItem('e2e-open-settings'),
  Storage.getItem('e2e-ask-notifications'), Storage.getItem('e2e-update-every'), Storage.getItem('prefs')]).then(
  async ([n, pair, opens, ask, every, prefs]) => {
    const launch = Number(n ?? 0) + 1
    void Storage.setItem('e2e-launches', String(launch))
    if (ask !== '1' && !prefs) await Storage.setItem('prefs', JSON.stringify({ askedNotifications: true }))
    const settingsOn = opens ? Number(opens) : OPEN_SETTINGS_ON_LAUNCH
    start({ pairLink: pair || PAIR, opensSettings: launch === settingsOn, updateEveryMs: every ? Number(every) : undefined })
  },
)
