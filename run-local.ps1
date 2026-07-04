# Run scanner service + frontend locally on Windows
# Requires: Docker, Go, Node/npm

$ErrorActionPreference = 'Stop'

# --- Postgres ---
docker stop scanner-postgres 2>$null; docker rm scanner-postgres 2>$null
docker run -d --name scanner-postgres `
  -e POSTGRES_USER=sws `
  -e POSTGRES_PASSWORD=sws `
  -e POSTGRES_DB=sws_scanner `
  -p 5433:5432 postgres:16-alpine | Out-Null
Write-Host "Postgres starting on port 5433..."
Start-Sleep 5

# --- Scanner service ---
$svcRoot = $PSScriptRoot
Set-Location $svcRoot
go build -o server.exe ./cmd/server
$env:PORT = '8088'
$env:DATABASE_URL = 'postgres://sws:sws@localhost:5433/sws_scanner?sslmode=disable'
$svc = Start-Process -FilePath '.\server.exe' -WorkingDirectory $svcRoot -PassThru -WindowStyle Hidden
Write-Host "Scanner service starting on http://localhost:8088 ..."
Start-Sleep 3

# Health check
try {
  $h = Invoke-RestMethod -Uri http://localhost:8088/healthz -TimeoutSec 5
  Write-Host "Service health: $($h.status)"
} catch {
  Write-Warning "Service health check failed: $_"
}

# --- Frontend ---
$feRoot = Join-Path $svcRoot '..' 'sws-scanner-app'
Set-Location $feRoot
if (-not (Test-Path node_modules)) {
  Write-Host "Installing frontend dependencies..."
  npm install
}
npm run build | Out-Null
$env:REACT_APP_API_BASE_URL = 'http://localhost:8088/v1'
$fe = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c npx serve -s build -l 3000' -WorkingDirectory $feRoot -PassThru -WindowStyle Hidden
Write-Host "Frontend starting on http://localhost:3000 ..."
Start-Sleep 5

try {
  $r = Invoke-WebRequest -Uri http://localhost:3000 -UseBasicParsing -TimeoutSec 5
  Write-Host "Frontend responded: $($r.StatusCode)"
} catch {
  Write-Warning "Frontend check failed: $_"
}

Write-Host ""
Write-Host "==================================="
Write-Host "Service : http://localhost:8088"
Write-Host "Frontend: http://localhost:3000"
Write-Host "==================================="
Write-Host "Press Enter to stop all processes..."
Read-Host

# --- Cleanup ---
Stop-Process -Id $fe.Id -Force -ErrorAction SilentlyContinue
Get-Process -Name node -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Stop-Process -Id $svc.Id -Force -ErrorAction SilentlyContinue
docker stop scanner-postgres | Out-Null; docker rm scanner-postgres | Out-Null
Write-Host "Stopped."
