[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [switch]$UpdateReadme,

    [switch]$SkipChecks
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Normalize-Version {
    param([string]$RawVersion)

    $trimmed = $RawVersion.Trim()
    if ($trimmed -eq "") {
        throw "Version cannot be empty."
    }

    if ($trimmed -notmatch "^v") {
        $trimmed = "v$trimmed"
    }

    if ($trimmed -notmatch "^v\d+\.\d+\.\d+$") {
        throw "Version must look like v0.3.0 or 0.3.0."
    }

    return $trimmed
}

function Require-Command {
    param([string]$Name)

    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($null -eq $command) {
        throw "Required command '$Name' was not found in PATH."
    }

    return $command.Source
}

function Invoke-External {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(Mandatory = $true)]
        [string[]]$ArgumentList
    )

    & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed: $FilePath $($ArgumentList -join ' ')"
    }
}

function Get-GitOutput {
    param([string[]]$ArgumentList)

    $output = & git @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "git $($ArgumentList -join ' ') failed."
    }

    return ($output | Out-String).Trim()
}

function Get-Latest-Tag {
    try {
        $tag = Get-GitOutput -ArgumentList @("describe", "--tags", "--abbrev=0")
        if ($tag -ne "") {
            return $tag
        }
    } catch {
        return ""
    }

    return ""
}

function Get-Existing-Tag {
    param([string]$TagName)

    $tag = Get-GitOutput -ArgumentList @("tag", "-l", $TagName)
    return $tag
}

function Update-ReadmeReleaseLinks {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ReadmePath,

        [Parameter(Mandatory = $true)]
        [string]$ReleaseVersion
    )

    $content = Get-Content $ReadmePath -Raw

    $linuxLine = '- Linux [`amd64`](https://github.com/olivier-w/climp/releases/download/__VERSION__/climp___VERSION___linux_amd64.tar.gz), [`arm64`](https://github.com/olivier-w/climp/releases/download/__VERSION__/climp___VERSION___linux_arm64.tar.gz)'.Replace('__VERSION__', $ReleaseVersion)
    $darwinLine = '- macOS [`amd64` (intel)](https://github.com/olivier-w/climp/releases/download/__VERSION__/climp___VERSION___darwin_amd64.tar.gz), [`arm64` (m1,m2,m3,m4,m5)](https://github.com/olivier-w/climp/releases/download/__VERSION__/climp___VERSION___darwin_arm64.tar.gz)'.Replace('__VERSION__', $ReleaseVersion)
    $windowsLine = '- Windows [`amd64`](https://github.com/olivier-w/climp/releases/download/__VERSION__/climp___VERSION___windows_amd64.zip)'.Replace('__VERSION__', $ReleaseVersion)

    $updated = $content
    $updated = [regex]::Replace($updated, '(?m)^- Linux .*$'  , [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $linuxLine }, 1)
    $updated = [regex]::Replace($updated, '(?m)^- macOS .*$'  , [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $darwinLine }, 1)
    $updated = [regex]::Replace($updated, '(?m)^- Windows .*$', [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $windowsLine }, 1)

    if ($updated -eq $content) {
        Write-Host "README release links already match $ReleaseVersion."
        return
    }

    if ($PSCmdlet.ShouldProcess($ReadmePath, "Update README release links to $ReleaseVersion")) {
        $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
        [System.IO.File]::WriteAllText($ReadmePath, $updated, $utf8NoBom)
        Write-Host "Updated README release links to $ReleaseVersion."
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot

try {
    $normalizedVersion = Normalize-Version -RawVersion $Version
    $goPath = Require-Command -Name "go"
    Require-Command -Name "git" | Out-Null
    Require-Command -Name "gh" | Out-Null

    $currentBranch = Get-GitOutput -ArgumentList @("branch", "--show-current")
    $ignoredStatus = Get-GitOutput -ArgumentList @("status", "--short", "--", "README.md", "RELEASING.md", "scripts/release.ps1")
    $blockingStatus = Get-GitOutput -ArgumentList @("status", "--short", "--", ".", ":(exclude)README.md", ":(exclude)RELEASING.md", ":(exclude)scripts/release.ps1")
    $latestTag = Get-Latest-Tag
    $existingTag = Get-Existing-Tag -TagName $normalizedVersion

    Write-Host "Repository root: $repoRoot"
    Write-Host "Target version:   $normalizedVersion"
    Write-Host "Current branch:   $currentBranch"
    if ($latestTag -ne "") {
        Write-Host "Latest tag:       $latestTag"
    } else {
        Write-Host "Latest tag:       <none>"
    }

    if ($currentBranch -ne "main") {
        throw "Release prep must start from the 'main' branch."
    }

    if ($blockingStatus -ne "") {
        throw "Working tree has non-release-prep changes. Commit or stash them before running release prep.`n$blockingStatus"
    }

    if ($ignoredStatus -ne "") {
        Write-Host "Ignoring release-prep changes:"
        foreach ($line in ($ignoredStatus -split "`r?`n")) {
            Write-Host "  $line"
        }
    }

    if ($existingTag -ne "") {
        throw "Tag $normalizedVersion already exists."
    }

    if ($latestTag -ne "") {
        Write-Host ""
        Write-Host "Commits since ${latestTag}:"
        Invoke-External -FilePath "git" -ArgumentList @("log", "--oneline", "$latestTag..HEAD")
    }

    if (-not $SkipChecks) {
        Write-Host ""
        Write-Host "Running release checks..."
        Invoke-External -FilePath $goPath -ArgumentList @("build", "-o", "climp.exe", ".")
        Invoke-External -FilePath $goPath -ArgumentList @("vet", "./...")
        Invoke-External -FilePath $goPath -ArgumentList @("test", "./...")
    }

    if ($UpdateReadme) {
        Write-Host ""
        Update-ReadmeReleaseLinks -ReadmePath (Join-Path $repoRoot "README.md") -ReleaseVersion $normalizedVersion
    }

    Write-Host ""
    Write-Host "Next steps:"
    Write-Host "  git add README.md RELEASING.md scripts/release.ps1"
    Write-Host "  git commit -m ""docs: prepare $normalizedVersion release"""
    Write-Host "  git push origin main"
    Write-Host "  git tag $normalizedVersion"
    Write-Host "  git push origin $normalizedVersion"
    Write-Host "  gh release view $normalizedVersion --repo olivier-w/climp"
    Write-Host '  $env:GITHUB_TOKEN = "<token with repo access>"'
    Write-Host "  goreleaser release --clean"
} finally {
    Pop-Location
}
