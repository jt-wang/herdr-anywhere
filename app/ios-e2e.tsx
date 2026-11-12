// The app e2e/ios.sh installs: the real app, opened at the run's pairing link (a script cannot tap the simulator;
// nsh.json must sit next to the entry, so it is here). For the same reason it opens on Settings on the launch
// e2e/open-settings.ts names.
// A script can also hand both over after the build, in the app's storage (storage.json in its data container, written
// before the launch): e2e-pair and e2e-open-settings, read here first. e2e/ios-tailcat.sh does that, since a Tailcat
// invite lasts 2 minutes and a build after it opened took longer on CI. It keeps each check of the line when back in the
// front in e2e-probes, which e2e/ios-resume.sh reads.
import { Storage } from 'nsh'
import { start } from './src/main.tsx'
import { OPEN_SETTINGS_ON_LAUNCH } from './e2e/open-settings.ts'
import { PAIR } from './e2e/pair.ts'

void Promise.all([Storage.getItem('e2e-launches'), Storage.getItem('e2e-pair'), Storage.getItem('e2e-open-settings')]).then(
  ([n, pair, opens]) => {
    const launch = Number(n ?? 0) + 1
    void Storage.setItem('e2e-launches', String(launch))
    const settingsOn = opens ? Number(opens) : OPEN_SETTINGS_ON_LAUNCH
    // Each check of the line when back in the front, kept for e2e/ios-resume.sh: [ms, answered, when].
    const probes: [number, boolean, number][] = []
    const onProbe = (ms: number, answered: boolean) => { probes.push([ms, answered, Date.now()]); void Storage.setItem('e2e-probes', JSON.stringify(probes)) }
    start({ pairLink: pair || PAIR, opensSettings: launch === settingsOn, onProbe })
  },
)
