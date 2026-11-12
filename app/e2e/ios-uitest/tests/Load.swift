import Foundation
// The machine's load for each note (the 1-minute average, what sysctl vm.loadavg gives): a simulator's processes run
// on the Mac's own kernel, so on CI it is the runner's. The suites sample the same from their side
// (e2e/ios-failure-logs.sh --sample-load), kept with a failure, to check these against.
func runnerLoad() -> String {
  var a = [Double](repeating: 0, count: 3)
  return getloadavg(&a, 3) == 3 ? String(format: "load %.2f", a[0]) : "load ?"
}
