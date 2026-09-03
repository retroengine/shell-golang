# Test runner for PowerShell.  Usage: .\test.ps1 [mode]
param(
    [ValidateSet('unit', 'e2e', 'all', 'cover', 'strict', 'fuzz')]
    [string]$Mode = 'all',

    [string]$FuzzTime = '30s'
)

Set-Location (Join-Path $PSScriptRoot 'app')

# The test reporter prints Unicode tick/cross marks. Windows PowerShell 5.1
# renders them as mojibake unless the console is told the output is UTF-8.
# Set NO_COLOR=1 to drop the ANSI colours, or SHELL_TEST_ASCII=1 to fall back
# to plain [PASS] / [FAIL] markers.
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()

switch ($Mode) {
    'unit' {
        Write-Host '==> unit tests'
        go test -run TestHandle -v ./...
    }
    'e2e' {
        Write-Host '==> end-to-end tests'
        go test -run TestE2E -v ./...
    }
    'all' {
        Write-Host '==> vet'
        go vet ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Write-Host '==> all tests'
        go test -v ./...
    }
    'cover' {
        Write-Host '==> coverage'
        go test -v -coverprofile=coverage.out ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        go tool cover -func=coverage.out
        go tool cover -html=coverage.out -o coverage.html
        Write-Host '==> wrote app/coverage.html'
    }
    'strict' {
        # Shuffled and repeated: catches order dependence and flakes. These
        # tests change the working directory and HOME, so this is the run
        # that actually proves they are independent.
        Write-Host '==> vet'
        go vet ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Write-Host '==> strict: shuffled, 3 repeats'
        go test -v -shuffle=on -count=3 -timeout 5m ./...
    }
    'fuzz' {
        # Generates new inputs hunting for a crash. Ctrl-C to stop early; any
        # crash found is saved to testdata/fuzz/ and replays as a normal test.
        Write-Host "==> fuzzing each target for $FuzzTime"
        foreach ($target in @('FuzzHandleInput', 'FuzzHandleEcho', 'FuzzHandleTYPE')) {
            Write-Host "--- $target"
            go test -fuzz $target -fuzztime $FuzzTime -run '^$' ./...
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        }
    }
}

exit $LASTEXITCODE
