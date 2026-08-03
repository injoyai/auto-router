@echo off
REM Start auto-router frontend dev server
setlocal

cd /d "%~dp0web"

if not exist "node_modules" (
  echo [run-web] node_modules not found, installing dependencies...
  call npm install
  if errorlevel 1 (
    echo [run-web] Dependency installation failed. Please check network or npm config.
    exit /b 1
  )
)

echo [run-web] Starting frontend dev server (http://localhost:5173)...
call npm run dev

endlocal
