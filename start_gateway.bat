@echo off
setlocal EnableDelayedExpansion

cd /d C:\Robin

:: Load .env
for /F "usebackq tokens=1,* delims==" %%A in (".env") do (
    set "line=%%A"
    if not "!line:~0,1!"=="#" (
        if not "%%A"=="" (
            set "%%A=%%B"
        )
    )
)

::Set JWT keys
set ROBIN_JWT_PUBKEY_FILE=C:\Robin\config\keys\public.pem
set ROBIN_JWT_PRIVKEY_FILE=C:\Robin\config\keys\private.pem
set ROBIN_MASTER_KEY=717938fc33c5d5d5ce3824815ea41626f295893946337b0fe7ebfa944028b94b

echo [Gateway] Starting Go OMS on :8080 ...
echo [Gateway] ALPACA_API_KEY = %ALPACA_API_KEY%
echo [Gateway] JWT KEY = %ROBIN_JWT_PUBKEY_FILE%

cd services\gateway
go run .
