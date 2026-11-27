# Trial: on Windows, each agent typed into herdr-core's own shell pane, as in Herdr; then what herdr-core reports for
# the pane (`hc get --json`: its program in front and its status) and whether the phone lists it as an agent
# (windows-agents.ts, the app's own rule). Writes every pane's report and screen to $RUNNER_TEMP/agents.
$ErrorActionPreference = 'Stop'
$hc = $env:HM_HC
$out = Join-Path $env:RUNNER_TEMP 'agents'
New-Item -ItemType Directory -Force $out | Out-Null
$env:HC_SESSION = 'hmagents'
$env:PATH = "$env:HM_NPM;$env:PATH"
$work = Join-Path $env:RUNNER_TEMP 'work'
New-Item -ItemType Directory -Force $work | Out-Null

$native = Join-Path $HOME '.local\bin\claude.exe'
$cases = [ordered]@{
  'shell'          = ''
  # npm's own .cmd shims by their path: herdr-core's shell here lacks the npm folder a user's PATH has, and typed
  # bare the name isn't found. The path runs what a user's `claude` runs: the shim, then node and the agent.
  'npm-claude'     = "& '$env:HM_NPM\claude.cmd'"
  'npm-gemini'     = "& '$env:HM_NPM\gemini.cmd'"
  'npm-codex'      = "& '$env:HM_NPM\codex.cmd'"
}
if (Test-Path $native) { $cases['native-claude'] = "& '$native'" }

foreach ($name in $cases.Keys) {
  & $hc run --name $name --no-parent --bg --cwd $work | Out-Host
  if ($LASTEXITCODE) { throw "hc run $name`: $LASTEXITCODE" }
}
Start-Sleep -Seconds 5
foreach ($name in $cases.Keys) {
  if ($cases[$name]) {
    & $hc send $name $cases[$name] | Out-Host
    if ($LASTEXITCODE) { throw "hc send $name`: $LASTEXITCODE" }
  }
}
Start-Sleep -Seconds 40
foreach ($name in $cases.Keys) {
  & $hc get $name --json | Out-File -Encoding utf8 (Join-Path $out "$name.json")
  & $hc read $name | Out-File -Encoding utf8 (Join-Path $out "$name.screen.txt")
  Write-Host "== $name ($($cases[$name]))"
  Get-Content (Join-Path $out "$name.screen.txt") | Where-Object { $_.Trim() } | Select-Object -First 12 | ForEach-Object { "   | $_" }
}
node --experimental-strip-types app/e2e/ci/windows-agents.ts $out
$code = $LASTEXITCODE
& $hc server stop | Out-Host
exit $code
