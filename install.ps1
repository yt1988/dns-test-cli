# dnstest installer for Windows: downloads the build for this CPU from dns.1ms.dog (which passes the GitHub release
# files on) or else from GitHub, checks its SHA-256 against the release's checksums.txt, installs it as
# %LOCALAPPDATA%\Programs\dnstest\dnstest.exe, adds that folder to your PATH and starts the test with its menu.
#
#   irm https://dns.1ms.dog/i.ps1 | iex
#
# Environment: DNSTEST_VERSION (a tag such as v0.2.0; default latest), DNSTEST_DIR (install folder),
# DNSTEST_NO_RUN=1 (install only).
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # the progress bar makes Invoke-WebRequest many times slower

& {
  $repo = 'yt1988/dns-test-cli'
  $version = if ($env:DNSTEST_VERSION) { $env:DNSTEST_VERSION } else { 'latest' }
  $bases = if ($version -eq 'latest') {
    @('https://dns.1ms.dog/dl/latest', "https://github.com/$repo/releases/latest/download")
  } else {
    @("https://dns.1ms.dog/dl/$version", "https://github.com/$repo/releases/download/$version")
  }
  $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
  $file = "dnstest-windows-$arch.exe"
  $dir = if ($env:DNSTEST_DIR) { $env:DNSTEST_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\dnstest' }
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("dnstest-" + [guid]::NewGuid())
  New-Item -ItemType Directory -Force $tmp | Out-Null
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  try {
    Write-Host "Downloading $file ($version)..."
    $got = $false
    foreach ($base in $bases) {
      try {
        Invoke-WebRequest -UseBasicParsing "$base/$file" -OutFile "$tmp\dnstest.exe"
        Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile "$tmp\checksums.txt"
        $got = $true
        break
      } catch {
        Write-Host "  not from $base, trying the next source"
      }
    }
    if (-not $got) { throw "dnstest install: download failed from: $($bases -join ' ')" }

    $want = (Get-Content "$tmp\checksums.txt" | Where-Object { $_ -match " $([regex]::Escape($file))$" } | ForEach-Object { ($_ -split ' ')[0] }) | Select-Object -First 1
    if (-not $want) { throw "dnstest install: no checksum for $file" }
    $have = (Get-FileHash -Algorithm SHA256 "$tmp\dnstest.exe").Hash.ToLower()
    if ($have -ne $want) { throw "dnstest install: checksum mismatch for $file (expected $want, got $have)" }

    New-Item -ItemType Directory -Force $dir | Out-Null
    Move-Item -Force "$tmp\dnstest.exe" (Join-Path $dir 'dnstest.exe')
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }

  $exe = Join-Path $dir 'dnstest.exe'
  Write-Host "Installed $(& $exe --version) to $exe"
  # on the user's PATH for new windows, and on this one's now
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not (($userPath -split ';') -contains $dir)) {
    [Environment]::SetEnvironmentVariable('Path', ($(if ($userPath) { "$userPath;" } else { '' }) + $dir), 'User')
    Write-Host "Added $dir to your PATH."
  }
  if (-not (($env:Path -split ';') -contains $dir)) { $env:Path = "$env:Path;$dir" }
  Write-Host 'Run it again any time with: dnstest   (dnstest --help lists the options)'

  if (-not $env:DNSTEST_NO_RUN) {
    Write-Host ''
    & $exe
  }
}
