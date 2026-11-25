# Install decsr-tui from the latest GitHub release (or DECSR_VERSION).
#
#   irm https://raw.githubusercontent.com/Decsr/decsr-tui/main/scripts/install.ps1 | iex
#
# Environment:
#   DECSR_VERSION      release tag to install, e.g. v0.7.1 (default: latest)
#   DECSR_INSTALL_DIR  install directory (default: %LOCALAPPDATA%\Programs\decsr-tui)
#
# No administrator rights needed: it writes to your user profile and your user PATH.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Install-DecsrTui {
    $repo = 'Decsr/decsr-tui'
    $bin = 'decsr-tui'

    # Windows PowerShell 5.1 defaults to old TLS versions that GitHub rejects.
    try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch { }
    $ProgressPreference = 'SilentlyContinue'

    $archId = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch -Regex ($archId) {
        '^(AMD64|x86_64)$' { $arch = 'amd64' }
        '^ARM64$' { $arch = 'arm64' }
        default { throw "unsupported architecture: $archId (supported: amd64, arm64)" }
    }

    $tag = $env:DECSR_VERSION
    if (-not $tag) {
        try {
            $rel = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'decsr-tui-installer' }
            $tag = $rel.tag_name
        } catch {
            throw "could not determine the latest release (is one published? GitHub API rate limit? set `$env:DECSR_VERSION, e.g. 'v0.7.1'): $($_.Exception.Message)"
        }
    }
    if (-not $tag) { throw 'could not determine the latest release' }
    if (-not $tag.StartsWith('v')) { $tag = "v$tag" }
    $version = $tag.Substring(1)

    $archive = "${bin}_${version}_windows_${arch}.zip"
    $base = "https://github.com/$repo/releases/download/$tag"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("decsr-tui-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading $bin $tag (windows/$arch)..."
        $zip = Join-Path $tmp $archive
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $zip
        } catch {
            throw "no release asset $archive for $tag (see https://github.com/$repo/releases): $($_.Exception.Message)"
        }
        $sums = Join-Path $tmp 'checksums.txt'
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $sums
        } catch {
            throw "could not download checksums.txt for ${tag}: $($_.Exception.Message)"
        }

        $want = $null
        foreach ($line in Get-Content -LiteralPath $sums) {
            $parts = @($line.Trim() -split '\s+')
            if ($parts.Count -ge 2 -and $parts[1].TrimStart('*') -eq $archive) { $want = $parts[0]; break }
        }
        if (-not $want) { throw "$archive not listed in checksums.txt" }
        $got = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash
        if ($want.ToLowerInvariant() -ne $got.ToLowerInvariant()) {
            throw "checksum mismatch for $archive (expected $want, got $got)"
        }

        $x = Join-Path $tmp 'x'
        Expand-Archive -LiteralPath $zip -DestinationPath $x -Force
        $exe = Join-Path $x "$bin.exe"
        if (-not (Test-Path -LiteralPath $exe)) { throw "$bin.exe not found in $archive" }

        $dir = $env:DECSR_INSTALL_DIR
        if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\decsr-tui' }
        try {
            New-Item -ItemType Directory -Path $dir -Force | Out-Null
            Copy-Item -LiteralPath $exe -Destination (Join-Path $dir "$bin.exe") -Force
        } catch {
            throw "cannot write to $dir (set `$env:DECSR_INSTALL_DIR to a writable directory; close any running $bin): $($_.Exception.Message)"
        }
        Write-Host "Installed $bin $tag to $(Join-Path $dir "$bin.exe")"

        # Add the directory to the user PATH only if it is missing.
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not $userPath) { $userPath = '' }
        $full = [IO.Path]::GetFullPath($dir).TrimEnd('\')
        $present = $false
        foreach ($p in ($userPath -split ';')) {
            if ($p -and ([Environment]::ExpandEnvironmentVariables($p).TrimEnd('\') -ieq $full)) { $present = $true; break }
        }
        if ($present) {
            Write-Host "$dir is already on your PATH."
        } else {
            $newPath = if ($userPath) { $userPath.TrimEnd(';') + ';' + $full } else { $full }
            [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
            Write-Host "Added $full to your user PATH. Open a new terminal for it to take effect."
        }
        Write-Host ''
        Write-Host "Next: run 'decsr-tui' inside a git repository."
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Install-DecsrTui
