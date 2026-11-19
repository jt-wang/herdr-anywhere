# Add phone in the Herdr Desktop App on Windows, as a person gets it: Herdr and herdr-core from Herdr's own installer,
# the released companion from INSTALL.md's PowerShell steps as written (its SHA256 checked against the release's SHA256
# file), then the dialog in Herdr's real window. Reads each window frame (`herdr-app shot`) with Windows' OCR and
# decodes its QR code (frame.py). Runs on a GitHub Windows runner, in three phases, so a phone in another job can pair
# in between:
#
#   dialog  install; ctrl-shift-m does nothing before install-dialog; the dialog offers Start; Start brings up the
#           companion and a code that decodes to an invite; Escape closes the dialog and the companion stays.
#   code    ctrl-shift-m opens the dialog again; its new code's invite goes to hm-secret\invite (see below) for the phone's job.
#   phone   waits for the phone: it pairs, the dialog says so, and what it typed runs in a terminal here; then this
#           computer removes it, and the companion outlives Herdr.
#
#   powershell -File app/e2e/windows-add-phone.ps1 -Phase dialog -Out <folder>
#
# Windows PowerShell 5.1, for its WinRT OCR. <Out> gets masked screenshots (every QR code painted over) and logs with
# no invite in them; the invite's only file is in hm-secret in the runner's temp folder, which is never uploaded.
param(
    [Parameter(Mandatory)][ValidateSet("dialog", "code", "phone")][string]$Phase,
    [Parameter(Mandatory)][string]$Out
)
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$repo = (Resolve-Path "$PSScriptRoot\..\..").Path
$release = "https://github.com/jt-wang/herdr-anywhere/releases/latest/download"
$companion = Join-Path $env:LOCALAPPDATA "Programs\herdr-anywhere\herdr-anywhere.exe"
$appHome = Join-Path $env:USERPROFILE ".config\herdr-app"
$state = Join-Path $env:USERPROFILE ".config\herdr-anywhere"
# Outside <Out>, which is uploaded: the invite's only file, and the raw frames.
$secret = Join-Path $(if ($env:RUNNER_TEMP) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }) "hm-secret"
$frames = Join-Path $Out "frames"
New-Item -ItemType Directory -Force $Out, $secret, $frames | Out-Null

# A native command's output with its errors, as text. Windows PowerShell turns each stderr line of a redirected native
# command into an error, which "Stop" would throw on.
function Native([scriptblock]$command) {
    $ErrorActionPreference = "Continue"
    $text = & $command 2>&1 | ForEach-Object { "$_" } | Out-String
    $script:nativeExit = $LASTEXITCODE
    $text
}
function Fail($why) { Write-Host "FAIL: $why"; exit 1 }
function Ok($what) { Write-Host "ok: $what" }
# Herdr's installers add their folders to the user's PATH in the registry; a new step starts with the old one.
function Refresh-Path {
    $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")
}

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class Win {
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X; public int Y; }
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint x, uint y, uint data, UIntPtr extra);
    [DllImport("user32.dll")] public static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
    [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
}
"@

# ---- the window: keys and clicks reach the window in front

function Window {
    $p = Get-Process herdr-app -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
    if (-not $p) { Fail "no Herdr window" }
    $p.MainWindowHandle
}
function Front {
    $h = Window
    for ($i = 0; $i -lt 10; $i++) {
        # A process that isn't in front may not bring another one there; a key press first lets it (Alt, which does
        # nothing on its own here).
        [Win]::keybd_event(0x12, 0, 0, [UIntPtr]::Zero); [Win]::keybd_event(0x12, 0, 2, [UIntPtr]::Zero)
        [Win]::ShowWindow($h, 9) | Out-Null
        [Win]::SetForegroundWindow($h) | Out-Null
        Start-Sleep -Milliseconds 300
        if ([Win]::GetForegroundWindow() -eq $h) { return }
    }
    Fail "the Herdr window won't come to the front"
}
# Virtual-key codes: 0x11 Ctrl, 0x10 Shift, 0x4D M, 0x1B Escape, 0x0D Enter.
function Keys([byte[]]$vks) {
    Front
    foreach ($k in $vks) { [Win]::keybd_event($k, 0, 0, [UIntPtr]::Zero); Start-Sleep -Milliseconds 30 }
    [array]::Reverse($vks)
    foreach ($k in $vks) { [Win]::keybd_event($k, 0, 2, [UIntPtr]::Zero); Start-Sleep -Milliseconds 30 }
    Start-Sleep -Milliseconds 700
}
function CtrlShiftM { Keys @(0x11, 0x10, 0x4D) }
# A point on the frame is a point in the window's client area: the frame is drawn at full pixel size.
function Click($x, $y) {
    Front
    $p = New-Object Win+POINT
    $p.X = [int]$x; $p.Y = [int]$y
    [Win]::ClientToScreen((Window), [ref]$p) | Out-Null
    [Win]::SetCursorPos($p.X, $p.Y) | Out-Null
    Start-Sleep -Milliseconds 200
    [Win]::mouse_event(2, 0, 0, 0, [UIntPtr]::Zero); Start-Sleep -Milliseconds 80; [Win]::mouse_event(4, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 700
}
# herdr-app's CLI output, as text. The Windows build is a windowed program that attaches to its caller's console
# (AttachConsole), so a PowerShell pipe can come back empty (on CI: `herdr-app --version` printed nothing):
# the first way that reads `status` is kept.
$script:appWay = $null
function App {
    $a = $args
    $exe = (Get-Command herdr-app).Source
    $file = Join-Path $secret "app-out.txt"
    Remove-Item $file, "$file.err" -ErrorAction SilentlyContinue
    $ways = if ($script:appWay) { @($script:appWay) } else { @("pipe", "cmd", "start") }
    foreach ($way in $ways) {
        switch ($way) {
            "pipe" { $text = Native { & $exe @a } }
            "cmd" { Native { & cmd.exe /c "`"$exe`" $($a -join ' ') > `"$file`" 2>&1" } | Out-Null; $text = Get-Content -Raw $file -ErrorAction SilentlyContinue }
            "start" {
                $p = Start-Process -FilePath $exe -ArgumentList $a -NoNewWindow -Wait -PassThru -RedirectStandardOutput $file -RedirectStandardError "$file.err"
                $script:nativeExit = $p.ExitCode
                $text = (Get-Content -Raw $file -ErrorAction SilentlyContinue) + (Get-Content -Raw "$file.err" -ErrorAction SilentlyContinue)
            }
        }
        if ($script:appWay -or "$text".Trim()) { if (-not $script:appWay -and $a[0] -eq "status") { $script:appWay = $way; Write-Host "herdr-app's output is read by: $way" }; return "$text" }
    }
    ""
}
function Status {
    $t = App status
    if (-not $t.Trim()) { Fail "herdr-app status printed nothing, read any way" }
    $t
}
function Overlays { @((Status) -split "`n" | Select-String -SimpleMatch '"add-phone"').Count }

# ---- reading a frame: Windows' OCR (WinRT) for the words, frame.py for the QR code

Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
$null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics, ContentType = WindowsRuntime]
$asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() |
    Where-Object { $_.Name -eq "AsTask" -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } |
    Select-Object -First 1
function Await($op, [Type]$type) {
    $task = $asTask.MakeGenericMethod($type).Invoke($null, @($op))
    $task.Wait(-1) | Out-Null
    $task.Result
}
$ocr = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
if (-not $ocr) { $ocr = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage((New-Object Windows.Globalization.Language "en-US")) }
if (-not $ocr) { Fail "Windows has no OCR language here: $([Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages | ForEach-Object LanguageTag)" }

$script:n = 0
# Saves a frame and reads it: $script:lines (text, x, y of each line's words, in frame pixels) and $script:qr (how
# many codes). The invite of a code on it is in $secret\frame-invite until the next frame.
function Frame {
    $script:n++
    $shot = Join-Path $secret "shot.png"
    App shot $shot | Out-Null
    if (-not (Test-Path $shot)) { Fail "herdr-app shot saved no frame" }
    $masked = Join-Path $frames ("{0:d3}.png" -f $script:n)
    $big = Join-Path $secret "ocr.png"
    $btn = Join-Path $secret "buttons"
    $codes = & python "$repo\app\e2e\frame.py" $shot (Join-Path $secret "frame-invite") $masked $big $btn
    if ($LASTEXITCODE) { Fail "frame.py failed" }
    $script:qr = @($codes | Where-Object { $_ -like "qr *" }).Count
    $found = @(Read-Text $big)
    Remove-Item $shot, $big -Force
    # ocr.png is twice the frame's size.
    $lines = @($found | ForEach-Object {
        $words = @($_.Words)
        [pscustomobject]@{ Text = $_.Text; Words = @($words | ForEach-Object {
            [pscustomobject]@{ Text = $_.Text; X = ($_.BoundingRect.X + $_.BoundingRect.Width / 2) / 2; Y = ($_.BoundingRect.Y + $_.BoundingRect.Height / 2) / 2 } }) }
    })
    # Each button's words, at the button's centre.
    foreach ($b in @($codes | Where-Object { $_ -like "button *" })) {
        $f = $b -split " "
        $xy = $f[2] -split ","
        $text = (@(Read-Text (Join-Path $btn "$($f[1]).png")) | ForEach-Object Text) -join " "
        if ($text) {
            $lines += [pscustomobject]@{ Text = $text; Words = @([pscustomobject]@{ Text = $text; X = [int]$xy[0]; Y = [int]$xy[1] }) }
        }
    }
    $script:lines = $lines
}
function Read-Text($image) {
    $file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($image)) ([Windows.Storage.StorageFile])
    $stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
    try {
        $decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
        $bitmap = Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
        @((Await ($ocr.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])).Lines)
    } finally { $stream.Dispose() }
}
function Says($text) { [bool]($script:lines | Where-Object { $_.Text -like "*$text*" }) }
function Shown { ($script:lines | ForEach-Object { "  | " + $_.Text }) -join "`n" }
function WaitFor($text, $seconds = 60) {
    $end = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $end) {
        Frame
        if (Says $text) { return }
        Start-Sleep -Seconds 1
    }
    Fail "the window never showed `"$text`"; it shows:`n$(Shown)"
}
function Keep($name) { Copy-Item (Join-Path $frames ("{0:d3}.png" -f $script:n)) (Join-Path $Out $name) }
function Invite {
    $i = (Get-Content -Raw (Join-Path $secret "frame-invite") -ErrorAction SilentlyContinue)
    if ($i) { $i = $i.Trim() }
    $i
}
# Every later log line hides it, should anything print it.
function Mask($i) { Write-Host "::add-mask::$i" }

# ---- a doc's PowerShell block, as an agent runs it: a fresh powershell with the default error handling

function Block($file, $want) {
    $text = Get-Content -Raw (Join-Path $repo $file)
    foreach ($m in [regex]::Matches($text, '(?ms)^ *```powershell\r?\n(.*?)^ *```')) {
        if ($m.Groups[1].Value.Contains($want)) { return ($m.Groups[1].Value -replace '(?m)^   ', '') }
    }
    Fail "$file has no PowerShell block with $want"
}
function Run-Block($file, $want) {
    Refresh-Path
    $b = Block $file $want
    Write-Host "--- $file, the block with $want"
    $output = Native { & powershell -NoProfile -Command $b }
    $script:blockExit = $script:nativeExit
    Write-Host $output.TrimEnd()
    $output
}

if ($Phase -eq "dialog") {
    # Herdr's installer, as herdr.dev gives it: `irm https://releases.herdr.dev/app/install.ps1 | iex`.
    Write-Host "--- Herdr's installer"
    Invoke-RestMethod https://releases.herdr.dev/app/install.ps1 | Invoke-Expression
    Refresh-Path
    $app = (Get-Command herdr-app -ErrorAction SilentlyContinue).Source
    if (-not $app) { Fail "Herdr's installer left no herdr-app on PATH" }
    Write-Host "herdr-app: $((App --version).Trim())"

    $out1 = Run-Block INSTALL.md "herdr-core --version"
    if ($script:blockExit -or $out1 -notmatch '\d+\.\d+\.\d+') { Fail "INSTALL.md step 1 found no herdr-core" }
    Ok "INSTALL.md step 1: herdr-core is installed"

    $out2 = Run-Block INSTALL.md "herdr-anywhere-windows-amd64.exe"
    $tag = ((curl.exe -fsSLI -o NUL -w "%{url_effective}" https://github.com/jt-wang/herdr-anywhere/releases/latest) -split "/")[-1]
    if ($out2 -notmatch "herdr-anywhere $([regex]::Escape($tag.TrimStart('v')))") { Fail "INSTALL.md step 2 did not print the latest release's version ($tag)" }
    $sums = curl.exe -fsSL "$release/SHA256"
    $want = ($sums | Where-Object { $_ -match '\sherdr-anywhere-windows-amd64\.exe$' } | ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    if (-not $want) { Fail "the release's SHA256 file lists no herdr-anywhere-windows-amd64.exe" }
    $got = (Get-FileHash $companion -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want.ToLower()) { Fail "the downloaded companion's SHA256 is $got; the release's SHA256 file says $want" }
    Ok "INSTALL.md step 2: the companion of $tag, SHA256 $got as the release's SHA256 file says"

    # The first window skips the welcome screen, which would cover the dialog.
    New-Item -ItemType Directory -Force $appHome | Out-Null
    '{"frame.welcomed":"skipped"}' | Set-Content -Encoding ascii (Join-Path $appHome "shared.json")
    Start-Process $app -WorkingDirectory $env:USERPROFILE
    $end = (Get-Date).AddSeconds(60)
    while ((Get-Date) -lt $end) {
        $t = App status
        if ($t.Trim() -and (Test-Path (Join-Path $appHome "keys.json"))) { break }
        Start-Sleep -Seconds 1
    }
    if (-not (App status).Trim()) { Fail "the Herdr window never answered herdr-app status" }
    $t = (Status) -replace '\s+', ' '
    Write-Host "status: $($t.Substring(0, [Math]::Min(400, $t.Length)))"
    Start-Sleep -Seconds 3
    $h = Window
    Write-Host "window $h, dpi $([Win]::GetDpiForWindow($h))"

    # The check that the key opens the dialog, seen failing: before install-dialog, ctrl-shift-m opens nothing.
    Frame
    if ($script:lines.Count -eq 0) { Fail "OCR reads nothing on Herdr's window" }
    if (Says "running on this computer") { Fail "the dialog text is on screen before the dialog" }
    if ($script:qr) { Fail "a QR code is on screen before the dialog" }
    Keep "0-herdr.png"
    CtrlShiftM
    Start-Sleep -Seconds 2
    if ((Overlays) -ne 0) { Fail "ctrl-shift-m opened Add phone before install-dialog" }
    Ok "before install-dialog: OCR reads the window ($($script:lines.Count) lines), no QR code, and ctrl-shift-m opens nothing"

    $out3 = Run-Block INSTALL.md "install-dialog"
    if ($out3 -notmatch "Add phone is installed\. Press ctrl-shift-m in Herdr") { Fail "INSTALL.md step 3 did not install the dialog" }
    if ((Get-Content -Raw (Join-Path $appHome "keys.json")) -notmatch '"ctrl-shift-m":\s*"mod.open:add-phone"') { Fail "keys.json has no ctrl-shift-m for Add phone" }
    $c = App check (Join-Path $appHome "mods\add-phone")
    Write-Host $c.Trim()
    if ($script:nativeExit) { Fail "herdr-app check mods\add-phone" }
    Ok "INSTALL.md step 3: install-dialog added ctrl-shift-m and the mod, which loads"

    Write-Host "herdr-app do mod.open --name add-phone: $((App do mod.open --name add-phone).Trim())"
    if ((Overlays) -ne 1) { Write-Host "status after mod.open: $(Status)" }
    WaitFor "running on this computer"
    $start = $script:lines.Words | Where-Object { $_.Text -eq "Start" } | Select-Object -First 1
    if (-not $start) { Fail "the dialog offers no Start:`n$(Shown)" }
    if ($script:qr) { Fail "a QR code before the companion runs" }
    Keep "1-offers-start.png"
    Ok "no companion yet, and the dialog offers Start"

    Click $start.X $start.Y
    $how = "a click on Start"
    $end = (Get-Date).AddSeconds(15)
    do { Frame; if (Says "Starting") { break }; if (Says "scan this code") { break } } while ((Get-Date) -lt $end)
    if (-not ((Says "Starting") -or (Says "scan this code"))) {
        # A click that lands nowhere: Enter is the dialog's Start too (main.js: primary).
        Keys @(0x0D); $how = "Enter (the click did nothing)"
    }
    WaitFor "scan this code" 90
    if (-not (Get-Process herdr-anywhere -ErrorAction SilentlyContinue)) { Fail "no herdr-anywhere process after Start" }
    Keep "2-code.png"
    Ok "$how brought up the companion and a code"

    $invite = Invite
    if ($script:qr -ne 1 -or $invite -notmatch '^herdr[12]:[A-Za-z0-9_-]+$') { Fail "the frame's QR code did not decode to an invite ($($script:qr) codes)" }
    Mask $invite
    Ok "the code on screen decodes to an invite, $($invite.Length) characters"
    # The check itself first: it must find the invite where it is.
    if (-not (Select-String -Quiet -SimpleMatch $invite (Join-Path $secret "frame-invite"))) { Fail "the on-disk check finds nothing" }
    foreach ($dir in $appHome, $state) {
        # Every file that can be read: Herdr's home also holds its socket (seen on CI), which can't be.
        foreach ($f in Get-ChildItem -Recurse -File $dir -ErrorAction SilentlyContinue) {
            # Shared, as serve keeps its log open.
            try {
                $r = New-Object IO.StreamReader([IO.File]::Open($f.FullName, "Open", "Read", "ReadWrite"))
                try { $text = $r.ReadToEnd() } finally { $r.Close() }
            } catch { Write-Host "  not read: $($f.Name)"; continue }
            if ($text.Contains($invite)) { Fail "the invite is on disk in $($f.FullName)" }
        }
    }
    Ok "the invite is in no file of Herdr's home or the companion's state"

    for ($i = 0; $i -lt 5 -and (Overlays) -ne 0; $i++) { Keys @(0x1B); Start-Sleep -Seconds 1 }
    if ((Overlays) -ne 0) { Fail "Escape did not close the dialog" }
    Start-Sleep -Seconds 2
    if (-not (Get-Process herdr-anywhere -ErrorAction SilentlyContinue)) { Fail "the companion stopped with the dialog" }
    Ok "Escape closes the dialog, and the companion keeps running"

    # A terminal for the phone to find and type in, in a project folder, as Herdr's own spaces are.
    $project = Join-Path $env:USERPROFILE "src\acme-api"
    New-Item -ItemType Directory -Force $project | Out-Null
    & herdr-core run --bg --keep --cwd $project --label herdr.space=spwin --label herdr.tab=tbwin | Out-Null
    if ($LASTEXITCODE) { Fail "herdr-core run" }
    Ok "a terminal in acme-api for the phone"
}

if ($Phase -eq "code") {
    Refresh-Path
    for ($i = 0; $i -lt 5 -and (Overlays) -ne 1; $i++) { CtrlShiftM; Start-Sleep -Seconds 1 }
    if ((Overlays) -ne 1) { Fail "ctrl-shift-m did not open the dialog" }
    Ok "ctrl-shift-m opens the dialog"
    WaitFor "scan this code"
    $invite = Invite
    if ($script:qr -ne 1 -or $invite -notmatch '^herdr[12]:[A-Za-z0-9_-]+$') { Fail "the new code did not decode to an invite" }
    Mask $invite
    Copy-Item (Join-Path $secret "frame-invite") (Join-Path $secret "invite")
    Keep "3-code-for-the-phone.png"
    Ok "a new code, $($invite.Length) characters, for the phone"
}

if ($Phase -eq "phone") {
    Refresh-Path
    $project = Join-Path $env:USERPROFILE "src\acme-api"
    $typed = Join-Path $project "hm-typed"
    $log = Join-Path $state "serve.log"
    $end = (Get-Date).AddMinutes(10)
    $paired = $false
    while ((Get-Date) -lt $end) {
        Frame
        if (Says "is paired") { $paired = $true }
        if ($paired -and (Test-Path $typed)) { break }
        Start-Sleep -Seconds 3
    }
    if (-not $paired) { Fail "the dialog never said the phone is paired; it shows:`n$(Shown)" }
    $name = ($script:lines | Where-Object { $_.Text -like "*is paired*" } | Select-Object -First 1).Text
    Keep "4-paired.png"
    Ok "the dialog says: $name"
    # ForEach-Object: Windows PowerShell's ConvertFrom-Json passes a JSON array on as one item.
    $devices = @(& $companion devices --json | ConvertFrom-Json | ForEach-Object { $_ })
    if ($devices.Count -ne 1) { Fail "$($devices.Count) phones paired, not one" }
    if (-not (Select-String -Quiet -Pattern 'device .* connected \(binary,' $log)) { Fail "the companion logged no binary connection from the phone" }
    Ok "the companion has one phone, connected"
    if (-not (Test-Path $typed)) { Fail "what the phone typed did not run here (no $typed)" }
    if ((Get-Content -Raw $typed) -notmatch "phone") { Fail "hm-typed holds $(Get-Content -Raw $typed)" }
    Ok "the phone's command ran in the terminal here: hm-typed holds `"$((Get-Content -Raw $typed).Trim())`""

    & $companion revoke $devices[0].id | Out-Null
    if ($LASTEXITCODE) { Fail "revoke" }
    $left = @(& $companion devices --json | ConvertFrom-Json | ForEach-Object { $_ })
    if ($left.Count -ne 0) { Fail "the phone is still paired after revoke" }
    Ok "this computer removed the phone"

    Stop-Process -Name herdr-app
    Start-Sleep -Seconds 2
    if (-not (Get-Process herdr-anywhere -ErrorAction SilentlyContinue)) { Fail "the companion stopped with Herdr" }
    Ok "the companion outlives Herdr"
}
Write-Host "PASS ($Phase)"
