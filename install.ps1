[CmdletBinding()]
param(
    [Alias("OS")]
    [ValidateSet("windows", "linux", "darwin")]
    [string]$OperatingSystem,

    [string]$Version
)

$ErrorActionPreference = "Stop"
$Repository = "SABBAR-ZIYAD/OraLab"

function Get-DetectedOperatingSystem {
    if ($env:OS -eq "Windows_NT") {
        return "windows"
    }
    if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform(
            [System.Runtime.InteropServices.OSPlatform]::Linux)) {
        return "linux"
    }
    if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform(
            [System.Runtime.InteropServices.OSPlatform]::OSX)) {
        return "darwin"
    }
    return "unknown"
}

function Select-OperatingSystem([string]$Detected) {
    $Choices = @(
        @{ Name = "Windows (Intel/AMD 64-bit)"; Value = "windows" },
        @{ Name = "Linux (Intel/AMD 64-bit)"; Value = "linux" },
        @{ Name = "macOS (Intel 64-bit)"; Value = "darwin" }
    )
    $Selected = switch ($Detected) {
        "linux" { 1 }
        "darwin" { 2 }
        default { 0 }
    }

    Write-Host "OraLab installation wizard"
    Write-Host ""
    Write-Host "Choose your operating system with Up/Down, then press Enter:"
    $MenuTop = $Host.UI.RawUI.CursorPosition.Y

    while ($true) {
        $Host.UI.RawUI.CursorPosition = New-Object System.Management.Automation.Host.Coordinates 0, $MenuTop
        for ($Index = 0; $Index -lt $Choices.Count; $Index++) {
            $Suffix = if ($Choices[$Index].Value -eq $Detected) { " (detected)" } else { "" }
            $Prefix = if ($Index -eq $Selected) { "  > " } else { "    " }
            $Line = $Prefix + $Choices[$Index].Name + $Suffix
            Write-Host $Line.PadRight(55)
        }

        $Key = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
        switch ($Key.VirtualKeyCode) {
            38 { $Selected = ($Selected - 1 + $Choices.Count) % $Choices.Count }
            40 { $Selected = ($Selected + 1) % $Choices.Count }
            13 { return $Choices[$Selected].Value }
        }
    }
}

function Add-UnixPath([string]$InstallDirectory) {
    if (($env:PATH -split [IO.Path]::PathSeparator) -contains $InstallDirectory) {
        return $null
    }

    $PathLine = 'export PATH="$HOME/.local/bin:$PATH"'
    $ProfilePath = if ($env:SHELL -like "*/zsh") {
        Join-Path $HOME ".zshrc"
    } elseif ($env:SHELL -like "*/bash") {
        Join-Path $HOME ".bashrc"
    } else {
        Join-Path $HOME ".profile"
    }
    $Existing = if (Test-Path $ProfilePath) { Get-Content $ProfilePath } else { @() }
    if ($Existing -notcontains $PathLine) {
        Add-Content -Path $ProfilePath -Value "`n$PathLine"
    }
    $env:PATH = $InstallDirectory + [IO.Path]::PathSeparator + $env:PATH
    return $ProfilePath
}

$DetectedOperatingSystem = Get-DetectedOperatingSystem
if (-not $OperatingSystem) {
    if ([Console]::IsInputRedirected) {
        throw "Interactive input is unavailable. Pass -OS windows, -OS linux, or -OS darwin."
    }
    $OperatingSystem = Select-OperatingSystem $DetectedOperatingSystem
}
if ($OperatingSystem -ne $DetectedOperatingSystem) {
    throw "Selected $OperatingSystem, but this shell is running on $DetectedOperatingSystem. Run the installer on the computer where OraLab will be used and select that operating system."
}

$Architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
if ($Architecture -notin @("x64", "amd64")) {
    throw "OraLab currently supports only Intel/AMD 64-bit computers. Detected architecture: $Architecture"
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$Headers = @{
    Accept = "application/vnd.github+json"
    "User-Agent" = "OraLab-Installer"
}
$ReleaseEndpoint = if ($Version) {
    $RequestedTag = "v" + $Version.TrimStart("v")
    "https://api.github.com/repos/$Repository/releases/tags/$RequestedTag"
} else {
    "https://api.github.com/repos/$Repository/releases/latest"
}

Write-Host ""
Write-Host "Finding the OraLab release..."
$Release = Invoke-RestMethod -Uri $ReleaseEndpoint -Headers $Headers -UseBasicParsing
$Tag = [string]$Release.tag_name
if (-not $Tag) {
    throw "The GitHub release did not include a version tag."
}
$ReleaseVersion = $Tag.TrimStart("v")
$Extension = if ($OperatingSystem -eq "windows") { "zip" } else { "tar.gz" }
$AssetName = "oralab_${ReleaseVersion}_${OperatingSystem}_amd64.$Extension"
$Asset = $Release.assets | Where-Object { $_.name -eq $AssetName } | Select-Object -First 1
$ChecksumsAsset = $Release.assets | Where-Object { $_.name -eq "checksums.txt" } | Select-Object -First 1
if (-not $Asset) {
    throw "Release $Tag does not contain $AssetName."
}
if (-not $ChecksumsAsset) {
    throw "Release $Tag does not contain checksums.txt."
}

$TemporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("oralab-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TemporaryDirectory | Out-Null
try {
    $ArchivePath = Join-Path $TemporaryDirectory $AssetName
    $ChecksumsPath = Join-Path $TemporaryDirectory "checksums.txt"
    Write-Host "Downloading OraLab $ReleaseVersion for $OperatingSystem..."
    Invoke-WebRequest -Uri $Asset.browser_download_url -OutFile $ArchivePath -Headers $Headers -UseBasicParsing
    Invoke-WebRequest -Uri $ChecksumsAsset.browser_download_url -OutFile $ChecksumsPath -Headers $Headers -UseBasicParsing

    $EscapedAssetName = [Regex]::Escape($AssetName)
    $ChecksumLine = Get-Content $ChecksumsPath | Where-Object { $_ -match "^([a-fA-F0-9]{64})\s+\*?$EscapedAssetName$" } | Select-Object -First 1
    if (-not $ChecksumLine) {
        throw "$AssetName is missing from checksums.txt."
    }
    $ExpectedChecksum = ([Regex]::Match($ChecksumLine, "^[a-fA-F0-9]{64}")).Value.ToLowerInvariant()
    $ActualChecksum = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ActualChecksum -ne $ExpectedChecksum) {
        throw "Checksum verification failed for $AssetName."
    }
    Write-Host "[OK] Download verified"

    $ExtractDirectory = Join-Path $TemporaryDirectory "extracted"
    New-Item -ItemType Directory -Path $ExtractDirectory | Out-Null
    if ($OperatingSystem -eq "windows") {
        Expand-Archive -Path $ArchivePath -DestinationPath $ExtractDirectory
        $SourceExecutable = Join-Path $ExtractDirectory "oralab.exe"
    } else {
        & tar -xzf $ArchivePath -C $ExtractDirectory
        if ($LASTEXITCODE -ne 0) {
            throw "Could not extract $AssetName."
        }
        $SourceExecutable = Join-Path $ExtractDirectory "oralab"
    }
    if (-not (Test-Path $SourceExecutable -PathType Leaf)) {
        throw "The release archive does not contain the OraLab executable."
    }

    $ChangedProfile = $null
    if ($OperatingSystem -eq "windows") {
        $InstallDirectory = Join-Path $env:LOCALAPPDATA "Programs\OraLab"
        $Destination = Join-Path $InstallDirectory "oralab.exe"
        New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
        Copy-Item -Force $SourceExecutable $Destination

        $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $PathEntries = @($UserPath -split ";" | Where-Object { $_ })
        if ($PathEntries -notcontains $InstallDirectory) {
            $UpdatedPath = if ($UserPath) { "$UserPath;$InstallDirectory" } else { $InstallDirectory }
            [Environment]::SetEnvironmentVariable("Path", $UpdatedPath, "User")
        }
        if (($env:PATH -split ";") -notcontains $InstallDirectory) {
            $env:PATH = "$InstallDirectory;$env:PATH"
        }
    } else {
        $InstallDirectory = Join-Path $HOME ".local/bin"
        $Destination = Join-Path $InstallDirectory "oralab"
        New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
        Copy-Item -Force $SourceExecutable $Destination
        & chmod 755 $Destination
        if ($LASTEXITCODE -ne 0) {
            throw "Could not make $Destination executable."
        }
        $ChangedProfile = Add-UnixPath $InstallDirectory
    }

    Write-Host "[OK] Installed $Destination"
    & $Destination version
    if ($LASTEXITCODE -ne 0) {
        throw "OraLab was installed, but its version check failed."
    }
    Write-Host ""
    if ($ChangedProfile) {
        Write-Host "OraLab is installed. Restart your shell to load the updated PATH from $ChangedProfile."
    } elseif ($OperatingSystem -eq "windows") {
        Write-Host "OraLab is installed. New terminal windows will use the updated PATH."
    } else {
        Write-Host "OraLab is installed."
    }
    Write-Host "Next: create your lab directory and follow the README Quick start."
} finally {
    if (Test-Path $TemporaryDirectory) {
        Remove-Item -Recurse -Force $TemporaryDirectory
    }
}

