param(
    [string]$ApiBase = "http://127.0.0.1:8080",
    [string]$Account = "admin",
    [string]$Password = "admin123",
    [int]$RequestTimeoutSec = 0
)

if ($RequestTimeoutSec -le 0) {
    if (-not [string]::IsNullOrWhiteSpace($env:ZBOARD_SMOKE_TIMEOUT)) {
        try {
            $RequestTimeoutSec = [int]$env:ZBOARD_SMOKE_TIMEOUT
        } catch {
            $RequestTimeoutSec = 10
        }
    }
}
if ($RequestTimeoutSec -le 0) {
    $RequestTimeoutSec = 10
}

function Invoke-ZBoardApi($method, $path, $body = $null, $token = $null) {
  $headers = @{}
  if ($token) {
    $headers["Authorization"] = "Bearer $token"
  }

  $params = @{
    Uri         = "$ApiBase$path"
    Method      = $method
    Headers     = $headers
    ContentType = "application/json"
  }
  if ($body) {
    $params["Body"] = (ConvertTo-Json $body -Depth 5)
  }
  return Invoke-RestMethod @params -TimeoutSec $RequestTimeoutSec
}

Write-Output "Smoke test for zboard API..."

try {
  Invoke-ZBoardApi -method GET -path "/healthz" | Out-Null
  Write-Output "1) healthz ok"
} catch {
  Write-Error "healthz request failed: $($_.Exception.Message)"
  exit 1
}

try {
  $login = Invoke-ZBoardApi -method POST -path "/api/v1/auth/login" -body @{ account = $Account; password = $Password }
  $token = $login.auth.token
  Write-Output "2) login ok, user=$($login.user.username)"
} catch {
  Write-Error "login failed: $($_.Exception.Message)"
  exit 1
}

if (-not $token) {
  Write-Error "no token returned"
  exit 1
}

try {
  Invoke-ZBoardApi -method GET -path "/api/v1/auth/me" -token $token | Out-Null
  Write-Output "3) auth/me ok"
} catch {
  Write-Error "auth/me failed: $($_.Exception.Message)"
  exit 1
}

try {
  $plansResp = Invoke-ZBoardApi -method GET -path "/api/v1/plans" -token $token
  $planCount = if ($plansResp.data) { @($plansResp.data).Count } else { 0 }
  Write-Output "4) plans list ok, count=$planCount"
} catch {
  Write-Error "plans list failed: $($_.Exception.Message)"
  exit 1
}

try {
  $nodesResp = Invoke-ZBoardApi -method GET -path "/api/v1/nodes" -token $token
  $nodeCount = if ($nodesResp.data) { @($nodesResp.data).Count } else { 0 }
  Write-Output "5) nodes list ok, count=$nodeCount"
} catch {
  Write-Error "nodes list failed: $($_.Exception.Message)"
  exit 1
}

try {
  Invoke-ZBoardApi -method GET -path "/api/v1/traffic/summary" -token $token | Out-Null
  Write-Output "6) traffic summary ok"
} catch {
  Write-Error "traffic summary failed: $($_.Exception.Message)"
  exit 1
}

Write-Output "Smoke test done."
