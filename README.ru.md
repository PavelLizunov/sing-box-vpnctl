# sing-box-vpnctl

Форк [sing-box](https://github.com/SagerNet/sing-box), в который добавлены AmneziaWG и клиентский транспорт XHTTP. Это ядро [VPNRouter](https://github.com/PavelLizunov/VPNRouter) и [vpnctl](https://github.com/PavelLizunov/vpnctl) и готовая замена sing-box для тех, кому нужны эти два протокола.

**[Скачать последний выпуск](https://github.com/PavelLizunov/sing-box-vpnctl/releases/latest)** · [Все выпуски](https://github.com/PavelLizunov/sing-box-vpnctl/releases)

Платформы: Linux (amd64, arm64, armv7), Windows (amd64, arm64), macOS (универсальная сборка), Android (библиотека).

[English version](README.md)

## Что добавляет форк

- **AmneziaWG 2.0 и 3.1** в конечной точке WireGuard: мусорные пакеты, префиксы, диапазоны магических заголовков, сигнатурные пакеты с маскировкой под другие протоколы, случайные хвосты, защита заголовка, дополнение содержимого, интервал перевыпуска ключа. См. [параметры AmneziaWG](docs/vpnctl/awg.md).
- **Клиентский транспорт XHTTP** (Xray «splithttp») для VLESS и похожих исходящих подключений: режимы packet-up, stream-up и stream-one, повторное использование соединений с пределами. См. [параметры XHTTP](docs/vpnctl/xhttp.md).
- Поле `user` в выводе `/connections` Clash API и V2Ray stats API в сборках выпусков.

Всё остальное — sing-box без изменений.

## Быстрый старт

1. Скачайте архив для своей платформы из последнего выпуска и распакуйте.
2. Проверьте конфигурацию: `sing-box check -c config.json`
3. Запустите: `sing-box run -c config.json`

Формат конфигурации [тот же, что в оригинале](https://sing-box.sagernet.org/configuration/), плюс параметры ниже.

## Пример: конечная точка AmneziaWG

```json
{
  "type": "wireguard",
  "tag": "awg",
  "address": ["10.0.0.2/32"],
  "private_key": "<приватный ключ в base64>",
  "peers": [
    {
      "address": "198.51.100.1",
      "port": 51820,
      "public_key": "<публичный ключ в base64>",
      "allowed_ips": ["0.0.0.0/0"]
    }
  ],
  "jc": 4, "jmin": 40, "jmax": 70,
  "s1": 20, "s2": 30, "s3": 20, "s4": 30,
  "h1": 12345678, "h2": 23456789,
  "h3": 34567890, "h4": 45678901
}
```

Она помещается в список `endpoints`. Без параметров AmneziaWG это обычный WireGuard.

## Пример: исходящее подключение через XHTTP

```json
{
  "type": "vless",
  "tag": "xhttp-out",
  "server": "example.com",
  "server_port": 443,
  "uuid": "00000000-0000-0000-0000-000000000000",
  "tls": { "enabled": true, "server_name": "example.com" },
  "transport": {
    "type": "xhttp",
    "path": "/xhttp",
    "mode": "auto"
  }
}
```

## Ограничения

- XHTTP работает только как клиент и только по HTTP/2: h2 с TLS, h2c без него. Нет HTTP/1.1, нет HTTP/3, нет отдельных настроек для скачивания.
- AmneziaWG проверяется между двумя экземплярами этого ядра: рукопожатие и данные, каждое расширение отдельно и все вместе, на Linux, macOS и Windows. С официальной реализацией AmneziaWG и на устройстве Android здесь не проверялся.
- Истёкший срок чтения на соединении XHTTP завершает это соединение; продлить его после этого нельзя.

## Как форк следует за оригиналом

Форк меняет только свой слой. Каждый файл вне списка `release/FORK_OWNED` обязан побайтно совпадать с коммитом оригинала из `release/FORK_UPSTREAM_BASE`, иначе CI падает. Обновление ядра — это слияние следующего тега оригинала.

## Руководства

Страницы руководств на английском.

- [Параметры AmneziaWG](docs/vpnctl/awg.md)
- [Параметры XHTTP](docs/vpnctl/xhttp.md)
- [Рецепты DNS](docs/vpnctl/dns.md)
- [Сборки, выпуски и проверка скачанного](docs/vpnctl/build.md)
- [Архитектура](ARCHITECTURE.md)

## Лицензия

GPL-3.0-or-later, как у оригинала. См. [LICENSE](LICENSE).
