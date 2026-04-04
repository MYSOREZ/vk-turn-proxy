@echo off
chcp 65001 >nul
setlocal

:: Настройка параметров сборки для Android arm64
set GOOS=android
set GOARCH=arm64
:: Для сборки .so библиотеки (c-shared) CGO_ENABLED должен быть 1, 
:: но это требует наличия Android NDK в системе (CC/CXX).
:: Если вы собираете просто бинарник, оставьте CGO_ENABLED=0.
:: В данном случае, судя по расширению .so и названию, это библиотека.
set CGO_ENABLED=0

echo [1/3] Очистка...
if exist libvkturn.so del /f libvkturn.so
if exist libvkturn.h del /f libvkturn.h

echo [2/3] Сборка libvkturn.so...
:: Мы используем ./client, Go сам найдет все файлы (main.go, auth_vk.go, common.go и т.д.)
:: Примечание: для полноценного .so (JNI) обычно нужен -buildmode=c-shared и CGO_ENABLED=1 + NDK.
:: Если ваша цель - просто бинарник, названный как .so, текущие флаги подходят.
go build -v -ldflags "-s -w -checklinkname=0" -trimpath -o libvkturn.so ./client

if %ERRORLEVEL% NEQ 0 (
    echo [!] Ошибка при сборке!
    goto end
)

echo [3/3] Готово.
powershell -NoProfile -Command "echo ('Итоговый размер: ' + [math]::Round((Get-Item libvkturn.so).Length / 1MB, 2) + ' MB (' + (Get-Item libvkturn.so).Length + ' байт)')"

:end
pause
