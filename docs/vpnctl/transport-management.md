# Управление транспортами в sing-box, VPNRouter и vpnctl

Опциональный outbound можно проверить отдельной сборкой ядра и конфигурацией для своих узлов. Для этого пилота достаточно существующих реестров, dialer, маршрутизатора и API ядра. Для выдачи через подписки понадобятся изменения в VPNRouter и vpnctl. Клиент должен уметь распознать профиль, проверить возможности **своей** сборки и передать в sing-box нужную цепочку.

## Ревизии и границы ответственности

Срез исходников от 25 ноября 2026 года. Хэши веток `main` сверены с GitHub. Ниже описано поведение кода этих ревизий. План приёмки в конце документа относится к будущим проверкам на конкретной платформе.

| Компонент | Кто принимает решение | Где оно исполняется |
|---|---|---|
| sing-box-vpnctl `fe24739a`, база SagerNet `c005f353` (`v1.14.4`) | Реестры разбирают типы, менеджеры создают экземпляры по тегам. В форке уже есть AWG в WireGuard endpoint, клиентский XHTTP, нативный DNSTT, атрибуция пользователя и ограничение полосы. | Go-ядро принимает поток или пакет с TUN/inbound, применяет DNS и правила маршрута, вызывает outbound/endpoint. Изменения привязаны к границе форка. [База][base] · [Граница][boundary] · [Реестры][reg] |
| VPNRouter `aaa02215` | Клиент выбирает профиль, строит конфигурацию, владеет TUN и жизненным циклом подключения. Поддерживает импорт пользовательского JSON с последующей обработкой политик. | На десктопе управляет процессом sing-box. На Android ядро встроено через libbox, системный VPN и уведомления о сети обслуживает VPN-сервис приложения. [Генератор][gen] · [Пользовательский JSON][custom] · [Запуск][life] · [Android][android] |
| vpnctl `c0baf6ec` | Контроллер на Rust хранит инвентарь, разделяет серверный kernel и протокол, рендерит конфигурации, раздаёт профили с учётом клиента. | CLI и daemon делят реестр. Контроллер управляет серверным sing-box по SSH, проверяет и применяет конфигурацию через systemd. Выбор активного outbound на устройстве принадлежит VPNRouter. [Реестр vpnctl][vpnreg] · [Серверный kernel][vpnkernel] · [Выдача профиля][sub] |

### Что уже есть в ядре

- Обычный outbound получает конфигурацию через реестр. Штатный dialer разрешает имя сервера через заданный resolver и строит detour. Для адаптера с долгоживущей сессией есть `adapter.InterfaceUpdateListener`. При смене сети ядро закрывает отслеживаемые соединения и уведомляет зарегистрированных слушателей. DNSTT уже сбрасывает свою сессию таким способом. [Dialer][dialer] · [Сброс сети][network] · [DNSTT][dnstt]
- `selector` поддерживает явный выбор. `urltest` выбирает кандидатов с учётом поддерживаемой сети, истории и допуска задержки. Измерение делает HTTP HEAD через TCP. Даже ошибочный HTTP-статус может дать значение задержки. Отдельной проверки передачи UDP или содержимого ответа этот результат не даёт. Поле `now` в Clash API отражает выбор для TCP. Практическое следствие для UDP-носителя под WireGuard — проверять конечный путь через endpoint и отдельно гонять UDP. [Группа][group] · [Проба][probe] · [Clash proxies][proxies]
- `sing-box check` разбирает конфигурацию, создаёт и закрывает экземпляр ядра. Этим можно проверить синтаксис, наличие типа и часть ограничений. Пересылка, аутентификация пира и восстановление после смены сети требуют запуска. [Команда check][check]
- Обычный CI проверяет границу форка, гонки, AWG и взаимодействие DNSTT с тестовым сервером. Расширенный workflow содержит полный набор тестов, проверку модулей, кросс-сборки, проверку досягаемых уязвимостей и сборку Android AAR. Релиз выпускает Linux amd64/arm64/armv7, Windows amd64/arm64, macOS universal и Android AAR. Отдельная приёмка OpenWrt/разных ABI роутеров нужна до заявления о поддержке. [CI][ci] · [Verify][verify] · [Release][release]

## VPNRouter

| Область | Поведение в проверенной ревизии | Следствие для нового транспорта |
|---|---|---|
| Генерация и цепочки | В режиме generated есть раздельные ветки для endpoint и outbound, теги `proxy` и при необходимости `proxy-udp`. Пара DNSTT → SSH служит примером нативного транспорта к фиксированному backend. Генератор отклоняет запрос цепочки, если её цель стала недоступна. [Сборка цепочек][outbounds] | Для новой записи подписки нужен явный разбор. Неизвестное значение протокола в `BuildVlessOutbound` попадает в ветку VLESS. Проверять итоговый JSON также после пользовательского импорта, поскольку injector добавляет и нормализует правила. [Конструкторы][builders] · [Injector][custom] |
| TUN, DNS, маршруты | В формируемом TUN включаются настраиваемый auto-route и исключения, на Linux используются собственные table/rule index. В ядре активируется auto-detect интерфейса. Правило DNS hijack предшествует блокировке ICMP к адресу самого TUN, далее идут private/direct, правила UDP и маршруты приложений. Final зависит от full/split/include/exclude. [TUN][gen] · [Маршруты][route] | Описать, какие внутренние IP-пакеты несёт весь профиль и какие сокеты открывает внешний транспорт. Сетевые возможности отдельного носителя и верхнего VPN могут различаться. |
| DNS и IPv6 | DNS через `proxy` использует HTTPS или для ветки endpoint — UDP. Bootstrap и LAN имеют собственные правила. Режим, StrictDns и список приложений влияют на DNS final. В generated/split/include при недоступности прокси и StrictDns как единственной причине выбора VPN-DNS монитор после двух неудач пытается переключить DNS на direct, после двух успехов — вернуть; применение зависит от hot reload. Генератор TUN инициализирует список адресом IPv4, валидатор `ValidateDnsStrategyAndInbounds` требует `ipv4_only`. [DNS][dns] · [Монитор][monitor] · [Валидатор][leaks] | Любое заявление об отсутствии утечек привязывать к платформе, профилю и фазе reconnect. Проверить маршрутизацию IPv6-пакетов напрямую, включая устройство с глобальным адресом: отключение AAAA само по себе их не блокирует. |
| Доступность и failover | Для нескольких совместимых профилей формируется `urltest` с интервалом 3 минуты и допуском 150 мс. Автовосстановление VPNRouter учитывает активный выбор пользователя, отмену и поколение сессии. Есть предел в три попытки и бюджет 4 с максимум на восемь кандидатов. Исключения включают custom и активный DNSTT. [Генерация группы][builders] · [Автовыбор][failover] · [Смена поколения][generation] | Включать в автоматический пул только цепочки с общей политикой выхода. Для TCP-only профиля проверять явное отклонение неподдерживаемого трафика. При неизвестном результате пробы сохранять отдельный статус. |
| Перезапуск и ресурсы | На десктопе действуют блокировка владельца TUN, сериализация запуска/остановки и временный файл для рестарта с новой конфигурацией. `RestartWithConfig` публикует файл после подтверждения остановки. Менеджер задаёт дочернему процессу `GOMEMLIMIT=256MiB` по умолчанию и 64 MiB для deep verify при отсутствии заданного лимита. На Android используются callbacks ОС о смене сети. [Lifecycle][life] · [Android][android] | Переиспользовать имеющийся рестарт; подтвердить восстановление полезным трафиком. GOMEMLIMIT задаёт цель сборщика мусора и не измеряет RSS. |
| Диагностика и выбор | Клиент Clash API умеет получить список прокси, выбор группы, задержку, соединения, переключить selector. Headless уже имеет машиночитаемые `snapshot`, `servers.select/test/verify`, `diagnostics.check/export`. [API клиента][clash] · [Headless protocol v1][headless] | Для пилота подходят эти интерфейсы. Ошибки TLS, отказ аутентификации, отсутствие маршрута и неподдерживаемый тип должны различаться в диагностике. |

**Ограничение горячей перезагрузки.** В этой паре ревизий VPNRouter считает успешный HTTP-ответ на `PUT /configs?force=true` подтверждением применения файла. Проверенное ядро в `updateConfigs` сразу вызывает `render.NoContent`, не читая запроса. Поэтому HTTP 204 может оставить прежние маршруты и DNS активными. Для изменения графа транспортов в пилоте использовать управляемый рестарт и подтвердить новый тег и реальный путь трафика. Ручная смена уже сконфигурированной группы `selector` имеет собственный работающий обработчик. [Клиент hot reload][hot] · [Обработчик PUT][put] · [Обработчик selector][proxies]

## vpnctl

| Потребность оператора | Доступно сейчас | Что требуется для продукта |
|---|---|---|
| Список протоколов и возможности сборки | `vpnctl registry --output json` возвращает список kernel/protocol реестра контроллера. `vpnctl status SERVER --output json` читает по SSH активность и версию ядра узла. VPNRouter читает build tags AWG/XHTTP или определяет DNSTT по версии, для Android предусмотрен встроенный runtime. [CLI][cli] · [Реестр][vpnreg] · [Status][vpnkernel] · [Клиентские возможности][features] | Различать «рендерится контроллером», «доступен в установленной клиентской сборке» и «проверена связь». Для экспериментального build tag получать фактическую возможность от бинарника/библиотеки. |
| Валидация, применение, откат | `vpnctl render SERVER --kernel sing-box` рендерит серверный JSON из inventory. При deploy сервер проверяет временный файл своим `sing-box check`, сохраняет предыдущий файл, переименовывает новый, вызывает reload-or-restart и проверяет сервис и Stats API. При ошибке предусмотрена попытка восстановления предыдущей конфигурации, если она существовала. [Render][render] · [Применение][deploy] | Проверить маршрут через новый peer отдельно от статуса systemd и Stats API. Для client-only носителя оставить модель серверных kernels прежней. |
| Статистика и состояние | vpnctld получает состояния служб, версии, память и счётчики интерфейса. Есть опрос соединений Clash и сбор накопительных server/user counters через loopback Stats API и серверный helper. [Опрос узла][node] · [Clash poller][poller] · [Stats][stats] | Эти данные описывают управляемый сервер. Для локального активного тега и задержки использовать API конкретного клиента. Серверный helper не добавляет клиентских процессов. |
| Выдача подписок | `/api/v1/app/config` учитывает продукт, capability tokens, платформу и для поддерживаемых desktop-клиентов версию ядра. Есть `detour-v1`, `dnstt-v1`, `vmess-v1`, тесты побайтовой совместимости и заголовки `Vary`, `private, no-store`. [Формирование ответа][sub] · [Тесты подписки][compat] | Для новых автоматически раздаваемых типов добавить отдельную возможность и проверять её в существующем обработчике. Сохранить текущий ответ для старых клиентов и отключённой сборки. |
| Платформы узла | Стандартный stats-helper для управляемой установки отбирается для amd64 Linux. На другой архитектуре deploy требует указать соответствующий helper. [Проверка платформы][vpnkernel] | Сборка sing-box для ARM отдельно от установки всей управляемой роли на ARM/OpenWrt. Подтвердить каждый такой сценарий самостоятельно. |

## Приёмка и приоритеты

| Приоритет | Работа | Наблюдаемый результат |
|---|---|---|
| P0 | Для изменения графа в VPNRouter устранить ложное подтверждение hot reload. | На действующем ядре заменить тег A на B, запросить фактический список и отправить запрос на контролируемый приёмник через B. При неудачном старте сохранить рабочий прежний профиль; ошибка должна дойти до вызывающего кода. |
| P0 | Проверить границу опциональной сборки и подписок. | Обычные ключи и подписки дают прежнее поведение при включённом и отключённом теге. Установленная сборка с тегом принимает новый тип, сборка без него отклоняет конфигурацию до старта. Клиенты без возможности получают исходный ответ. |
| P1 | Проверить управляемый маршрут на двух контролируемых интерфейсах. | Раздельно проверить TCP, UDP (или явный отказ), DNS A/AAAA, IPv4/IPv6 и ICMP. Захват на внешнем интерфейсе показывает только разрешённые control/bootstrap/peer адреса и явно заданный direct-трафик. Проверить пустой период при потере сети, смену интерфейса и возобновление при восстановлении. |
| P1 | Сохранить различие между охватом порта, аутентификацией и работающим маршрутом. | Закрытый порт, неверный ключ, неверный TLS peer, таймаут разрешения имени и отказ тестового назначения дают различимые результаты. Задержка HTTP HEAD не маскирует сбой UDP. Диагностика обходится без ключей, URL с токенами и полного JSON. |
| P1 | Расширить клиентский генератор и серверную выдачу только при продуктовой интеграции. | Рендер проверен конечным ядром, существующий selected tag и правила DNS/TUN сохраняют смысл, build tag участвует в выдаче профиля. В админке и имеющемся API используются те же протоколы и типы ошибок. |
| P2 | Измерить цену включения на целевых устройствах. | Сравнить stripped-бинарник, RSS покоя/под нагрузкой, CPU, задержку первого соединения и reconnect на одинаковых Go, тегах, peer и профиле нагрузки. На Android отдельно измерять встроенную библиотеку и смену Wi-Fi/мобильной сети; на шлюзе — память, MTU и пропускную способность. |

[base]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/release/FORK_UPSTREAM_BASE
[boundary]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/release/FORK_OWNED
[reg]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/include/registry.go
[dialer]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/common/dialer/dialer.go
[network]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/route/network.go
[dnstt]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/protocol/dnstt/outbound.go
[group]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/protocol/group/urltest.go
[probe]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/common/urltest/urltest.go
[proxies]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/experimental/clashapi/proxies.go
[check]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/cmd/sing-box/cmd_check.go
[ci]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/.github/workflows/ci.yml
[verify]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/.github/workflows/verify.yml
[release]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/.github/workflows/release.yml
[put]: https://github.com/PavelLizunov/sing-box-vpnctl/blob/fe24739a8b8c1ebd573269e50b124366310e4d3e/experimental/clashapi/configs.go
[gen]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/ConfigGenerator.cs
[dns]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/ConfigGenerator.Dns.cs
[route]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/ConfigGenerator.Route.cs
[outbounds]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/ConfigGenerator.Outbounds.cs
[builders]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/ConfigGenerator.OutboundBuilders.cs
[custom]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/CustomConfigInjector.cs
[android]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Android/VpnRouterService.java
[life]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/SingBoxManager.Lifecycle.cs
[monitor]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/HealthMonitor.cs
[leaks]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/LeakProtection.cs
[failover]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/AutoFailoverEngine.cs
[generation]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/VpnEngine.Failover.cs
[clash]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/ClashSingBoxApi.cs
[headless]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Headless/Protocol/ProtocolConstants.cs
[hot]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/SingBoxManager.HotReload.cs
[features]: https://github.com/PavelLizunov/VPNRouter/blob/aaa02215764f1d17658c3b4b7a6da0566724687c/VPNRouter.Core/Services/SingBoxFeatures.cs
[vpnreg]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/crates/kernels/src/registry.rs
[vpnkernel]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/crates/kernels/src/sing_box.rs
[sub]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/daemon/src/handlers/vpn_router/routes.rs
[cli]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/cli/src/main.rs
[render]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/cli/src/cmd/render.rs
[deploy]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/crates/kernels/src/sing_box/scripts.rs
[node]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/daemon/src/node_probe.rs
[poller]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/daemon/src/clash_poller.rs
[stats]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/daemon/src/singbox_stats.rs
[compat]: https://github.com/PavelLizunov/vpnctl/blob/c0baf6ec65c65f4f6cbf860adee81c76930a4daf/daemon/tests/vpn_router_endpoint/client_detour_capability.rs
