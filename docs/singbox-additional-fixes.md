# Дополнительные исправления протоколов и sing-box

Ветка: `fix/singbox-protocol-audit`.
База этого этапа: `f7ca682a412e280f6315973483c0b88434474fae`.

Внесены изменения по 51 пункту дополнительного аудита (101–151). Это не подтверждение 200 новых независимых ошибок. Номера относятся к ранее составленному дополнительному аудиту. Там, где целевое ядро не поддерживает исходную семантику, исправление состоит в явном отказе или исключении несовместимого профиля.

## Коммиты

- `e6cc841`: native traffic, VMess inbound, ShadowTLS, процессы, DNS, QUIC TLS и API.
- `999e832`: импорт/экспорт ссылок VMess, Hysteria и Shadowsocks; простой native import.
- `589e9ef`: Clash TLS и протоколы, отключённые клиенты, REALITY SNI и HWID.
- `45cf09c`: конкурентный refresh, выборочные UPDATE и транзакционная смена приоритетов.
- `e5b120f`: строгие WireGuard bytes, native QUIC uTLS validation и TLS aliases.
- `10a1e1e`: Gecko/hopping и добавление подписок после пропусков приоритетов.

## Изменения по пунктам

| ID  | Дефект                                                                | Внесённое изменение                                                                                                                                       |
| --- | --------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 101 | VMess none/zero заменяются на auto                                    | Сохраняются исходные VMess security none/zero в обоих парсерах.                                                                                           |
| 102 | VMess-импорт теряет экспортируемый finalmask                          | VMess JSON fm разбирается общим обработчиком finalmask.                                                                                                   |
| 103 | VMess KCP теряет mtu и tti при импорте                                | VMess KCP mtu/tti переносятся с проверкой допустимых диапазонов.                                                                                          |
| 104 | Go VMess XHTTP переносит лишь три расширенных поля                    | Go VMess XHTTP переносит расширенные поля, вложенные объекты и режим.                                                                                     |
| 105 | Hysteria share link интерполирует секрет без URL escaping             | Hysteria auth кодируется encodeURIComponent перед созданием URL.                                                                                          |
| 106 | Hysteria2 auth с завершающим двоеточием обрезается                    | Наличие разделителя пароля проверяется по исходному authority; user: сохраняется.                                                                         |
| 107 | Legacy Shadowsocks пароль с @ разбирается как начало host             | Legacy Shadowsocks разделяет userinfo и адрес по последнему @.                                                                                            |
| 108 | V2Ray Stats int64 декодируется как ZigZag                             | Protobuf int64 читается Uvarint с последующим приведением к int64.                                                                                        |
| 109 | Проверка версии Snell срабатывает по произвольной строке              | Snell определяется по type входящих/исходящих обработчиков, а не строкам JSON.                                                                            |
| 110 | Stop игнорирует adopted процесс при сохранённом завершённом cmd       | Завершённый cmd не блокирует остановку принятого внешнего процесса.                                                                                       |
| 111 | Пустые объекты ошибочно считаются активными unsupported опциями       | Пустота объектов и массивов определяется рекурсивно.                                                                                                      |
| 112 | Handshake resolver REALITY/ShadowTLS ссылается на отсутствующий local | После native DNS patch восстанавливается local, если на него есть ссылки.                                                                                 |
| 113 | Валидаторы native Listable полей не принимают scalar                  | Валидаторы Listable принимают одиночную непустую строку.                                                                                                  |
| 114 | При ошибке Clash fallback сдвигается успешная граница Naive           | Граница Naive fallback обновляется только после успешного чтения Clash.                                                                                   |
| 115 | Clash клиент читает candidate config вместо applied config            | Clash и selector используют снимок конфигурации запущенного процесса.                                                                                     |
| 116 | Native sing-box импорт принимает несовместимый gRPC multiMode         | Несовместимые gRPC multiMode/authority явно отклоняются простым импортом.                                                                                 |
| 117 | Native импорт принимает Xray QUIC как sing-box QUIC                   | Xray QUIC transport явно отклоняется простым импортом.                                                                                                    |
| 118 | Native импорт ссылки молча снимает проверки ECH и pin                 | Непереносимые ECH/pin/MLDSA/finalmask параметры отклоняются; отдельное имя проверки не теряется молча.                                                    |
| 119 | Native REALITY профиль без fp не получает обязательный uTLS           | REALITY без fp получает chrome; unsafe отклоняется.                                                                                                       |
| 120 | Clash external VMess принудительно обнуляет alterId                   | Clash external VMess сохраняет alterId пользователя.                                                                                                      |
| 121 | Clash external VLESS теряет уровень шифрования                        | Clash external VLESS сохраняет encryption.                                                                                                                |
| 122 | External Hysteria2 для Clash теряет обфускацию и hopping              | Clash external Hysteria2 переносит salamander/Gecko, ports и hop-interval; неподдерживаемые маски исключаются.                                            |
| 123 | External Hysteria2 для Clash игнорирует insecure и pin                | Clash external Hysteria2 применяет общий обработчик проверки сертификатов.                                                                                |
| 124 | Существующий пустой HWID setting не заполняется                       | Пустой HWID заполняется условным UPDATE; возвращается фактически сохранённое значение.                                                                    |
| 125 | ShadowTLS-обёртка удаляет DB-авторизацию HTTP                         | ShadowTLS сохраняет уже переведённых DB-пользователей HTTP/Mixed.                                                                                         |
| 126 | ShadowTLS сервер и подписка используют разные пароли с пробелами      | ShadowTLS пароль сохраняется побайтно, включая окружающие пробелы.                                                                                        |
| 127 | Отключённые клиенты попадают в native JSON подписку                   | Отключённые клиенты исключаются из генерируемых JSON/Clash профилей; метаданные учитываются отдельно.                                                     |
| 128 | REALITY JSON не учитывает выбранный клиентский serverName             | REALITY сохраняет явно выбранный client serverName в JSON и Clash.                                                                                        |
| 129 | HTTP 200 с HTML стирает последний рабочий outbound snapshot           | Непустой ответ без валидных поддерживаемых ссылок возвращает ошибку; HTML не заменяет рабочий snapshot.                                                   |
| 130 | Редактирование подписки перезаписывает параллельный refresh           | Update записывает только редактируемые поля и намеренные сбросы балансовой политики.                                                                      |
| 131 | Более старый fetch может перезаписать новый snapshot                  | Fetch для одной подписки сериализован; запись перечитывается после захвата блокировки.                                                                    |
| 132 | Смена приоритета подписок не атомарна                                 | Move выполняется одной транзакцией с общей блокировкой изменения порядка.                                                                                 |
| 133 | Автобалансировка отвергает API на локальном LAN IP                    | Selector использует ту же проверку локального IP и нормализацию bind address, что Clash.                                                                  |
| 134 | VMess inbound использует неверное имя JSON поля alterId               | VMess inbound использует upstream JSON поле alterId.                                                                                                      |
| 135 | Native сборщик sing-box bytes не подключён к планировщику             | Cron вызывает PollTraffic до проверки websocket; lifecycle maintenance работает и при нулевых дельтах.                                                    |
| 136 | Native API snapshots упираются в gRPC 4 MiB                           | Лимит входящего gRPC сообщения увеличен до 64 MiB для API snapshots.                                                                                      |
| 137 | Simple editor отвергает TUIC с пустым паролем                         | TUIC с пустым паролем допускается простым редактором.                                                                                                     |
| 138 | HTTPS proxy link теряет SNI/insecure параметры                        | HTTPS proxy import сохраняет SNI, ALPN и allowInsecure/insecure.                                                                                          |
| 139 | WireGuard reserved: ошибочные элементы исчезают без отказа            | Reserved проверяется как массив из трёх целых байтов; ошибочные элементы не удаляются.                                                                    |
| 140 | Hysteria2 masquerade молча теряет insecure/xForwarded                 | Непереносимые masquerade insecure/xForwarded возвращают ошибку.                                                                                           |
| 141 | Standalone ShadowTLS меняет inbound tag без перевода routing          | Standalone ShadowTLS использует публичный тег на внутреннем listener для маршрутизации и учёта.                                                           |
| 142 | Mixed с ShadowTLS конфликтует с обязательным udp=false                | UDP=false у Mixed сохраняется обёрткой TCP ShadowTLS без конфликта с обычным mixed validator.                                                             |
| 143 | Clash общий TLS handler читает несовпадающую форму security полей     | Clash нормализует flat/nested TLS поля; полный SHA256 сертификата переносится в fingerprint.                                                              |
| 144 | Clash gRPC теряет multiMode/authority без проверки совместимости      | Clash исключает gRPC профили с multiMode/authority, которые не может воспроизвести.                                                                       |
| 145 | Clash external WireGuard не переносит reserved/MTU/keepalive          | Clash external WireGuard сохраняет reserved, MTU, keepalive и allowed-ips.                                                                                |
| 146 | Периодический refresh перезапускает ядро даже без изменений           | Периодический refresh сообщает об изменении конфигурации только при изменении outbound snapshot.                                                          |
| 147 | Adoption не распознаёт разрешённые global CLI flags перед run         | Adoption распознаёт global flags до/после run; неоднозначные конфигурации отвергаются.                                                                    |
| 148 | Bare IPv6 DNS address разбирается как host:port                       | Bare IPv6 DNS адрес оборачивается в скобки до разбора URL/порта.                                                                                          |
| 149 | Xray DNS tag игнорируется, политика egress меняется                   | Правила маршрутизации по Xray DNS virtual inbound tag явно отклоняются, поскольку их egress-политика не воспроизводится. Неиспользуемые теги допускаются. |
| 150 | Общий TLS translator включает несовместимый uTLS для QUIC             | TLS compatibility не включает TCP uTLS для QUIC; явно заданный native QUIC uTLS отвергается.                                                              |
| 151 | Go импорт TLS ссылок не читает allowInsecure/insecure                 | Go TLS URL parser сохраняет allowInsecure/insecure, включая false.                                                                                        |

## Проверка и ограничения

По указанию пользователя тесты, сборка и запуск ядер не проводились. Выполнены просмотр исходников и изменений, форматирование Go через gofmt и git diff --check. Компиляция, интеграция с установленными ядрами и сетевые сценарии остаются непроверенными.

- Исходная поддержка протоколов сверялась с sing-box v1.14.0 (`0b8995879f29a9b98ee027bc17b75e101445b238`) и sing-quic v0.7.0-beta.4 (`4ab2eceaac81e073f53b22ec72ff56aaea89a3d1`).
- Поля Clash сверялись с Mihomo Meta (`88dcbf7f1614a67c3b36b848ee3592dfa92ada36`), в том числе full-certificate fingerprint, Gecko, hopping и WireGuard options.
- Простой импорт sing-box и Clash не добавляют отсутствующие возможности ядра. В частности, остаются отказы для несовместимых gRPC опций и непереносимых проверок сертификата.
- Mihomo принимает один полный certificate fingerprint. Несколько альтернативных Xray pins отклоняются, чтобы не снимать проверки.
- Лимит gRPC snapshot — 64 MiB. Это конечная граница размера, а не поддержка неограниченного ответа.
- Refresh, редактирование и удаление одной подписки сериализованы внутри процесса; запрос редактирования может ждать завершения HTTP fetch с таймаутом 30 секунд. Межпроцессная координация нескольких экземпляров панели не добавлена.
- Учёт Naive использует Clash fallback; последние байты сессии, исчезнувшей между опросами, по отсутствующей записи восстановить нельзя.
- Ранее описанные ограничения в [singbox-protocol-fixes.md](singbox-protocol-fixes.md) остаются применимыми.

Источники схем: [sing-box](https://github.com/SagerNet/sing-box/tree/v1.14.0), [sing-quic](https://github.com/SagerNet/sing-quic), [Mihomo TLS](https://wiki.metacubex.one/en/config/proxies/tls/), [Hysteria2](https://wiki.metacubex.one/en/config/proxies/hysteria2/), [WireGuard](https://wiki.metacubex.one/en/config/proxies/wg/).
