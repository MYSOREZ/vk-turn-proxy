# VK TURN Proxy

VK TURN Proxy — клиент и сервер для прокидывания локального UDP-трафика через TURN-реле, получаемые из ссылки на VK Calls. Типичный сценарий — поднять `server` на VPS рядом с WireGuard, а на клиентском устройстве запустить `client`, который слушает локальный адрес вроде `127.0.0.1:9000`.

> [!CAUTION]
> Проект предназначен для обучения, исследований и администрирования собственных стендов. Используйте его только там, где у вас есть право запускать такой трафик и менять сетевую конфигурацию.

## Содержание

- [Как это работает](#как-это-работает)
- [Возможности](#возможности)
- [Что нужно](#что-нужно)
- [Быстрый старт: WireGuard](#быстрый-старт-wireguard)
  - [Запуск сервера на VPS](#1-запустите-сервер-на-vps)
  - [Настройка WireGuard](#2-настройте-wireguard-на-клиенте)
  - [Запуск клиента](#3-запустите-клиент)
- [Android](#android)
- [systemd-сервис](#сервер-как-systemd-сервис)
- [Docker](#docker)
- [VLESS / Xray](#vless--xray)
- [Флаги клиента](#флаги-клиента)
- [Флаги сервера](#флаги-сервера)
- [Сборка из исходников](#сборка-из-исходников)
- [Решение проблем](#решение-проблем)
- [Похожие проекты](#похожие-проекты)
- [Лицензия](#лицензия)

## Как Это Работает

Схема для WireGuard:

```text
WireGuard client → 127.0.0.1:9000 → VK TURN Proxy client
  → VK TURN relay → VK TURN Proxy server на VPS
  → 127.0.0.1:51820 → WireGuard server
```

Клиент берёт временные TURN-учётные данные из ссылки VK Calls, открывает несколько DTLS-соединений к TURN-реле и отправляет через них трафик к `server` на VPS. Трафик обфусцируется под WebRTC OPUS-аудио (RTP-заголовки + ChaCha20-Poly1305 AEAD), что делает его неотличимым от реального звонка.

## Возможности

- VK Calls как источник TURN-учётных данных (2 стабильных app_id, ротация).
- Несколько параллельных TURN-потоков через `-n` (кратно 9).
- RTP-AEAD обфускация DTLS-пакетов: трафик похож на WebRTC OPUS для DPI.
- Ключ шифрования выводится из пароля через HKDF — никаких hex-ключей.
- DTLS Connection ID для снижения DPI-отпечатка.
- Автоматическое прохождение VK captcha (v2, checkbox).
- WireGuard UDP backend.
- VLESS/Xray TCP backend через `-vless`.
- Docker-образ для серверной части.

## Что Нужно

- VPS с публичным IP.
- На VPS уже должен слушать backend:
  - WireGuard: обычно `127.0.0.1:51820/udp`;
  - Xray/VLESS: обычно `127.0.0.1:443/tcp`.
- Ссылка на активный VK Calls вида `https://vk.com/call/join/...`.
- На клиенте: WireGuard или другой локальный клиент, который будет ходить в `127.0.0.1:9000`.

## Быстрый Старт: WireGuard

### 1. Запустите Сервер На VPS

Скачайте бинарник для Linux amd64:

```bash
curl -L -o server https://github.com/MYSOREZ/vk-turn-proxy/releases/latest/download/server-linux-amd64
chmod +x server
```

Запустите сервер, указав адрес WireGuard и пароль:

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:51820 -wrap -password "ВАШ_ПАРОЛЬ"
```

Порт `56000/udp` должен быть доступен снаружи. Если WireGuard слушает другой порт — замените `51820`. Пароль может быть любой строкой — главное, чтобы он совпадал на клиенте и сервере.

### 2. Настройте WireGuard На Клиенте

В клиентском конфиге WireGuard замените endpoint сервера на локальный адрес VK TURN Proxy:

```ini
[Peer]
Endpoint = 127.0.0.1:9000
# ...

[Interface]
MTU = 1280
# ...
```

### 3. Запустите Клиент

Linux:

```bash
curl -L -o client https://github.com/MYSOREZ/vk-turn-proxy/releases/latest/download/client-linux-amd64
chmod +x client
./client -peer 1.2.3.4:56000 -vk "https://vk.ru/call/join/ВАШ_ХЕШ" -password "ВАШ_ПАРОЛЬ" -listen 127.0.0.1:9000 -n 9
```

Windows PowerShell:

```powershell
Invoke-WebRequest -Uri https://github.com/MYSOREZ/vk-turn-proxy/releases/latest/download/client-windows-amd64.exe -OutFile client.exe
.\client.exe -peer 1.2.3.4:56000 -vk "https://vk.ru/call/join/ВАШ_ХЕШ" -password "ВАШ_ПАРОЛЬ" -listen 127.0.0.1:9000 -n 9
```

macOS:

```bash
curl -L -o client https://github.com/MYSOREZ/vk-turn-proxy/releases/latest/download/client-darwin-arm64
chmod +x client
./client -peer 1.2.3.4:56000 -vk "https://vk.ru/call/join/ВАШ_ХЕШ" -password "ВАШ_ПАРОЛЬ" -listen 127.0.0.1:9000 -n 9
```

После запуска клиента включите WireGuard.

> `-vk` принимает как полную ссылку, так и только хеш (часть после `/call/join/`). Несколько ссылок через запятую: `-vk "ХЕШ1,ХЕШ2"`.

## Android

Скачайте бинарник `client-android-arm64` из Releases и запустите через Termux или встройте в своё приложение:

```bash
termux-wake-lock
./client -peer 1.2.3.4:56000 -vk "https://vk.ru/call/join/ВАШ_ХЕШ" -password "ВАШ_ПАРОЛЬ" -listen 127.0.0.1:9000 -n 9
```

В WireGuard укажите `Endpoint = 127.0.0.1:9000` и `MTU = 1280`. Добавьте Termux в исключения WireGuard (split tunneling).

## Сервер Как systemd-Сервис

Пример `/etc/systemd/system/vk-turn-proxy.service`:

```ini
[Unit]
Description=VK TURN Proxy server
After=network.target

[Service]
Type=simple
ExecStart=/opt/vk-turn-proxy/server -listen 0.0.0.0:56000 -connect 127.0.0.1:51820 -wrap -password "ВАШ_ПАРОЛЬ"
Restart=always
RestartSec=5
User=nobody
Group=nogroup

[Install]
WantedBy=multi-user.target
```

Применить:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now vk-turn-proxy.service
sudo systemctl status vk-turn-proxy.service
```

## Docker

Образ публикуется в GitHub Container Registry:

```bash
docker pull ghcr.io/mysorez/vk-turn-proxy:latest
```

Host network (рекомендуется, если backend на том же хосте):

```bash
docker run --rm --network host \
  ghcr.io/mysorez/vk-turn-proxy:latest \
  -listen 0.0.0.0:56000 -connect 127.0.0.1:51820 -wrap -password "ВАШ_ПАРОЛЬ"
```

Bridge mode:

```bash
docker run --rm -p 56000:56000/udp \
  ghcr.io/mysorez/vk-turn-proxy:latest \
  -listen 0.0.0.0:56000 -connect HOST_IP:51820 -wrap -password "ВАШ_ПАРОЛЬ"
```

## VLESS / Xray

В режиме `-vless` `server` подключается к локальному TCP backend (например Xray).

Сервер:

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443 -vless -wrap -password "ВАШ_ПАРОЛЬ"
```

Клиент — для VLESS используйте отдельную сборку с поддержкой TCP (см. вашу конфигурацию).

С bonding (несколько параллельных DTLS через одно TCP):

```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:443 -vless -vless-bond -wrap -password "ВАШ_ПАРОЛЬ"
```

## Флаги Клиента

| Флаг | По умолчанию | Описание |
| --- | --- | --- |
| `-peer` | **обязательный** | адрес сервера на VPS, например `1.2.3.4:56000` |
| `-vk` | **обязательный** | ссылка(и) VK Calls или хеш(и), через запятую |
| `-password` | **обязательный** | пароль для шифрования (совпадает на клиенте и сервере) |
| `-listen` | `127.0.0.1:9000` | локальный адрес для WireGuard или другого клиента |
| `-n` | `24` | количество воркеров, кратно 9 (минимум 9) |
| `-ping-only` | `false` | замерить RTT через TURN и выйти |
| `-device-id` | `unknown` | ID устройства (для логов) |
| `-captcha-mode` | `auto` | режим капчи: `auto`, `wv` (WebView), `rjs` (Go solver) |
| `-vk-auth` | `anonymous` | режим авторизации VK: `anonymous` или `account` |
| `-vk-creds-file` | пусто | JSON-файл с TURN-кредами от аккаунта VK |
| `-stats-interval` | `30` | интервал статистики в секундах (`0` = выключить) |
| `-turn` | из ссылки | переопределить IP TURN-сервера |
| `-port` | из ссылки | переопределить порт TURN-сервера |

## Флаги Сервера

| Флаг | По умолчанию | Описание |
| --- | --- | --- |
| `-listen` | `0.0.0.0:56000` | адрес прослушивания |
| `-connect` | **обязательный** | backend-адрес: `127.0.0.1:51820` (WireGuard) или `127.0.0.1:443` (VLESS) |
| `-wrap` | `false` | включить RTP-AEAD обфускацию (требует `-password`) |
| `-password` | пусто | пароль для `-wrap`, ключ выводится через HKDF |
| `-vless` | `false` | TCP/VLESS режим |
| `-vless-bond` | `false` | bonding для VLESS |
| `-debug` | `false` | подробные логи |

## Сборка Из Исходников

Нужен Go 1.22+.

```bash
go build -o client ./client
go build -o server ./server
```

Кросс-сборка для Linux amd64:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o server-linux-amd64 ./server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o client-linux-amd64 ./client
```

Кросс-сборка для Android arm64 (требует NDK):

```bash
export CC=$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android21-clang
GOOS=android GOARCH=arm64 CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o client-android-arm64 ./client
```

## Решение Проблем

- Сначала запускайте VK TURN Proxy client, потом включайте WireGuard.
- Убедитесь, что `-password` одинаковый на клиенте и сервере.
- Если соединение не устанавливается — проверьте, что порт `56000/udp` открыт на VPS (firewall, ufw).
- Если WireGuard забирает весь трафик — добавьте IP TURN-реле в исключения маршрутизации.
- Если соединение нестабильное — попробуйте уменьшить `-n 9` (минимум).
- Если VK просит капчу слишком часто — попробуйте сменить ссылку VK Calls.
- Если клиент завис на получении кредов — проверьте, что ссылка VK Calls живая.
- Для отладки добавьте `-stats-interval 5` на клиенте, `-debug` на сервере.

## Похожие Проекты

Авторы этого репозитория не отвечают за работу сторонних проектов.

Server:

- https://github.com/Urtyom-Alyanov/turn-proxy — реализация на Rust.
- https://github.com/NedgNDG/vk-proxy-auto-installer — автоустановщик VK TURN Proxy.

Android:

- https://github.com/samosvalishe/turn-proxy-android
- https://github.com/MYSOREZ/vk-turn-proxy-android
- https://github.com/kiper292/wireguard-turn-android
- https://github.com/WINGS-N/WINGSV
- https://github.com/oxsidee/vkpn
- https://github.com/amurcanov/proxy-turn-vk-android

iOS:

- https://github.com/nullcstring/turnbridge

macOS:

- https://github.com/denny4-user/vk-turn-proxy-macos-gui

## Лицензия

GPL-3.0. См. [LICENSE](LICENSE).

<a href="https://www.star-history.com/?repos=MYSOREZ%2Fvk-turn-proxy&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=MYSOREZ/vk-turn-proxy&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=MYSOREZ/vk-turn-proxy&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=MYSOREZ/vk-turn-proxy&type=date&legend=top-left" />
 </picture>
</a>
