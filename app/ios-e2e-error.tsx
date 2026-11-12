// The app e2e/ios-onerror.sh installs: ios-e2e.tsx (the real app at the run's pairing link), and 10 s after it starts
// a timer's callback throws an error nothing catches. nsh hands that error to the app on the main thread; the app has
// to stay up and keep taking taps. Just before, it asks the Companion for /hm-e2e/throwing, which the Companion's log
// shows, so the test can tell a thrown error from a timer that never ran (NSH's iOS runtime keeps console.log to
// itself). The throw is in a timer of its own: one in a promise's callback would be a rejection, which nsh only logs.
import './ios-e2e.tsx'
import { PAIR } from './e2e/pair.ts'

setTimeout(() => {
  void fetch(new URL(PAIR).origin + '/hm-e2e/throwing')
    .catch(() => {})
    .finally(() => setTimeout(() => {
      throw new Error('hm-e2e: an error nothing catches')
    }, 0))
}, 10_000)
