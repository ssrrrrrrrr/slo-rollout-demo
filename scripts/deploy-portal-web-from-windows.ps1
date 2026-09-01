param(
  [string]$VmHost = "s-sentinel-host",
  [string]$VmUser = "root",
  [string]$RepoDir = "/root/slo-rollout-demo"
)

$ErrorActionPreference = "Stop"

Write-Output "===== build Portal frontend ====="
Push-Location web
pnpm run build
Pop-Location

Write-Output "===== upload dist to VM ====="
ssh "$VmUser@$VmHost" "rm -rf $RepoDir/web/dist"
scp -r web/dist "$VmUser@$VmHost`:$RepoDir/web/"

Write-Output "===== restart Portal Web on VM ====="
ssh "$VmUser@$VmHost" "cd $RepoDir && bash scripts/start-portal-web.sh && bash scripts/status-portal-web.sh"

Write-Output "===== Portal Web ready ====="
Write-Output "http://$VmHost`:18081"
