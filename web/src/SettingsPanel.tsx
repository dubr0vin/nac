import { t } from "./i18n";
import { useState } from "react";
import { Trans } from "react-i18next";
import {
  Alert,
  Accordion,
  CopyButton,
  Textarea,
  Anchor,
  Badge,
  Button,
  Code,
  Divider,
  FileButton,
  Group,
  Modal,
  NumberInput,
  Notification,
  Portal,
  Paper,
  Select,
  SimpleGrid,
  Stack,
  Tabs,
  Text,
  TextInput,
  Title,
  MultiSelect,
} from "@mantine/core";
import {
  api,
  readResponse,
  problemMessage,
  dayjs,
  effectiveTimezone,
  message,
  type Export,
  type Settings,
  type Source,
  type State,
} from "./api";
import example from "../../settings.example.yaml?raw";

function settingsPrompt() {
  return [
    t(
      "Ты помогаешь пользователю настроить календарь NAC через YAML. Отвечай на языке пользователя. Ниже полная схема поддерживаемых настроек; не придумывай другие поля или операторы.",
    ),
    t(
      "Если пользователь просит изменить настройки, сначала попроси его текущий YAML, если он ещё не прислал его. Затем сохрани всё, чего запрос не касается. Если просит настроить с нуля, используй базовый YAML ниже и примени его пожелания. Если пожеланий пока нет, спроси, что он хочет настроить. При нехватке существенной информации уточни её. Когда предлагаешь изменения, верни полный YAML одним блоком кода и кратко объясни результат: сохранение заменяет настройки целиком.",
    ),
    t(
      "Поля верхнего уровня:\n- tags: список названий личных тэгов (строки). Тэги одного события у разных людей независимы.\n- default_tags: объект с created и invited — списками тэгов для новых собственных событий и событий, в которые пользователя добавили другие. Существующие события не меняются.\n- busy: условие занятости; отменённые события не учитываются.\n- colors: непустой упорядоченный список объектов с when (условие) и color (цвет). Первое совпадение побеждает. Последний элемент обязательно имеет when: true и задаёт цвет по умолчанию.",
    ),
    t(
      "Условие — true (все события), false (никакие) или объект ровно с одним ключом:\n- tag: название — точное совпадение личного тэга с учётом регистра;\n- not: условие — отрицание;\n- and: [условия] — все должны совпасть; пустой список означает true;\n- or: [условия] — хотя бы одно должно совпасть; пустой список означает false.\nУсловия можно вкладывать друг в друга. Например: {and: [{tag: работа}, {not: {tag: отпуск}}]}.",
    ),
    t(
      'color: строка для сплошной заливки либо список из двух строк: заливка и цвет полоски слева с кружком перед названием, например [blue, orange]. Доступны имена Mantine (адаптируются к теме): dark, gray, grey, red, pink, grape, violet, indigo, blue, cyan, teal, green, lime, yellow, orange. grey — синоним gray. Также допустим HEX строго из шести цифр, обязательно в кавычках: "#fd7e14". Имена и HEX можно смешивать в паре.',
    ),
    t(
      'Префикс ics: зарезервирован для импортированных категорий: не добавляй такие тэги в tags или default_tags. В условиях можно ссылаться на существующие импортированные тэги, например {tag: "ics:Work"}. Часовой пояс и интервал обновления задаются в UI: timezone и refresh в YAML запрещены. Источники, экспорты, события и права доступа сюда не входят. Используй один YAML-документ и только перечисленные поля.',
    ),
    t("Базовые настройки для создания с нуля:"),
    `\`\`\`yaml
${example.trim()}
\`\`\``,
  ].join("\n\n");
}

const fields = () => [
  { value: "title", label: t("Название") },
  { value: "description", label: t("Описание") },
  { value: "location", label: t("Место") },
  { value: "url", label: t("Ссылка") },
  { value: "members", label: t("Участники") },
  { value: "tags", label: t("Мои тэги") },
];
const views = () => [
  { value: "day", label: t("День") },
  { value: "week", label: t("Неделя") },
  { value: "month", label: t("Месяц") },
  { value: "year", label: t("Год") },
  { value: "list", label: t("Список") },
];

function Timezone({
  value,
  onChange,
  automatic = false,
}: {
  automatic?: boolean;
  value: string;
  onChange: (zone: string) => void;
}) {
  return (
    <Select
      label={t("Часовой пояс")}
      searchable
      value={value}
      data={[
        ...(automatic ? [{ value: "", label: t("Как на устройстве") }] : []),
        ...[
          ...new Set(["UTC", value, ...Intl.supportedValuesOf("timeZone")]),
        ].filter(Boolean),
      ]}
      onChange={(value) => {
        if (value !== null) onChange(value);
      }}
    />
  );
}

export function SettingsPanel({
  state,
  onClose,
  onSaved,
}: {
  state: State;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [settings, setSettings] = useState<Settings>(() =>
    structuredClone(state.me.settings),
  );
  const [config, setConfig] = useState(state.me.settings.config ?? "");
  const [source, setSource] = useState<Source>();
  const [feed, setFeed] = useState<Export>();
  const [kiosk, setKiosk] = useState<Export>();
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [working, setWorking] = useState(false);
  const [token, setToken] = useState<{ id: string; value: string }>();
  const availableTags = [...new Set([...state.tags, ...settings.tags])];

  async function run(action: () => Promise<void>) {
    setWorking(true);
    setError("");
    setNotice("");
    try {
      await action();
      onSaved();
      setNotice(t("Сохранено"));
    } catch (error) {
      setError(message(error));
    } finally {
      setWorking(false);
    }
  }
  function newSource() {
    setSource({
      name: "",
      url: "",
      timezone: effectiveTimezone(settings.timezone),
      tags: [],
      interval: 900,
    });
  }
  function newFeed() {
    setFeed({
      name: "",
      condition: "true\n",
      fields: ["title", "description", "location", "url"],
      view: "week",
      theme: "auto",
      timezone: effectiveTimezone(settings.timezone),
      poll: 15,
    });
  }

  return (
    <>
      <Modal opened onClose={onClose} title={t("Настройки")} size="xl">
        <Stack>
          {error && <Alert color="red">{error}</Alert>}
          {notice && <Alert color="teal">{notice}</Alert>}
          <Tabs defaultValue="personal" keepMounted={false}>
            <Tabs.List mb="md">
              <Tabs.Tab value="personal">{t("Общие")}</Tabs.Tab>
              <Tabs.Tab value="sources">{t("Источники")}</Tabs.Tab>
              <Tabs.Tab value="exports">{t("Экспорт и киоск")}</Tabs.Tab>
            </Tabs.List>
            <Tabs.Panel value="personal">
              <Stack>
                <SimpleGrid
                  cols={{ base: 1, sm: 2 }}
                  style={{ alignItems: "end" }}
                >
                  <Timezone
                    automatic
                    value={settings.timezone}
                    onChange={(timezone) =>
                      setSettings({ ...settings, timezone })
                    }
                  />
                  <NumberInput
                    label={t("Обновление UI, секунды")}
                    min={5}
                    max={3600}
                    value={settings.poll}
                    onChange={(value) =>
                      setSettings({ ...settings, poll: Number(value) })
                    }
                  />
                </SimpleGrid>
                <Textarea
                  label="YAML"
                  aria-label={t("Личные настройки YAML")}
                  value={config}
                  onChange={(event) => setConfig(event.currentTarget.value)}
                  autosize
                  minRows={16}
                  maxRows={28}
                  spellCheck={false}
                  styles={{
                    input: {
                      fontFamily: "monospace",
                      fontSize: 13,
                      lineHeight: 1.6,
                    },
                  }}
                />
                <CopyButton value={settingsPrompt()} timeout={2500}>
                  {({ copied, copy }) => (
                    <>
                      <Text size="sm" c="dimmed">
                        <Trans
                          i18nKey="Не понимаете, как этим пользоваться? Скопируйте <prompt>промпт</prompt> и вставьте в любую LLM — она поможет разобраться."
                          components={{
                            prompt: (
                              <Anchor
                                component="button"
                                type="button"
                                onClick={copy}
                                inherit
                                underline="always"
                              />
                            ),
                          }}
                        />
                      </Text>
                      {copied && (
                        <Portal>
                          <Notification
                            role="status"
                            withCloseButton={false}
                            withBorder
                            style={{
                              position: "fixed",
                              bottom: 24,
                              right: 24,
                              zIndex: 1000,
                            }}
                          >
                            {t("Промпт скопирован")}
                          </Notification>
                        </Portal>
                      )}
                    </>
                  )}
                </CopyButton>
                <Accordion variant="default">
                  <Accordion.Item value="help">
                    <Accordion.Control>{t("Пример YAML")}</Accordion.Control>
                    <Accordion.Panel>
                      <Code block>{example}</Code>
                    </Accordion.Panel>
                  </Accordion.Item>
                </Accordion>
                <Button
                  loading={working}
                  onClick={() =>
                    run(async () => {
                      const saved = await api<Settings>(
                        "/api/settings/config",
                        "PUT",
                        {
                          yaml: config,
                          timezone: settings.timezone,
                          poll: settings.poll,
                        },
                      );
                      setSettings(saved);
                    })
                  }
                >
                  {t("Сохранить настройки")}
                </Button>
              </Stack>
            </Tabs.Panel>
            <Tabs.Panel value="sources">
              <Stack>
                <Text size="sm" c="dimmed">
                  {t(
                    "URL, файл и push заменяют полный снимок одного источника. Ваши личные тэги сохраняются.",
                  )}
                </Text>
                {state.sources.map((item) => (
                  <Paper withBorder p="md" key={item.id}>
                    <Group justify="space-between">
                      <div>
                        <Text fw={600}>{item.name}</Text>
                        <Text size="xs" c="dimmed">
                          {item.lastSuccess &&
                          !item.lastSuccess.startsWith("0001")
                            ? t("Обновлён {{time}}", {
                                time: dayjs(item.lastSuccess).format(
                                  "D MMM, HH:mm",
                                ),
                              })
                            : t("Ещё не загружен")}
                        </Text>
                      </div>
                      <Group gap="xs">
                        <Button
                          size="compact-sm"
                          variant="default"
                          onClick={() =>
                            setSource((current) =>
                              current?.id === item.id
                                ? undefined
                                : structuredClone(item),
                            )
                          }
                        >
                          {t("Настроить")}
                        </Button>
                        {item.url && (
                          <Button
                            size="compact-sm"
                            variant="light"
                            disabled={working}
                            onClick={() =>
                              run(async () => {
                                await api(
                                  `/api/sources/${item.id}/refresh`,
                                  "POST",
                                );
                              })
                            }
                          >
                            {t("Обновить")}
                          </Button>
                        )}
                        <FileButton
                          accept=".ics,text/calendar"
                          onChange={(file) => {
                            if (!file) return;
                            run(async () => {
                              const response = await fetch(
                                `/api/sources/${item.id}/import`,
                                {
                                  method: "POST",
                                  headers: {
                                    "X-NAC": "1",
                                    "Content-Type": "text/calendar",
                                  },
                                  body: file,
                                },
                              );
                              await readResponse(response);
                            });
                          }}
                        >
                          {(props) => (
                            <Button
                              {...props}
                              size="compact-sm"
                              variant="light"
                              disabled={working}
                            >
                              {t("Загрузить ICS")}
                            </Button>
                          )}
                        </FileButton>
                      </Group>
                    </Group>
                    {item.error && (
                      <Text c="red" size="sm" mt="xs">
                        {problemMessage(item.error)}
                      </Text>
                    )}
                  </Paper>
                ))}
                <Button variant="light" onClick={newSource}>
                  {t("+ Источник")}
                </Button>
                {source && (
                  <Paper withBorder p="md">
                    <Stack>
                      <Title order={4}>
                        {source.id
                          ? t("Настройки источника")
                          : t("Новый источник")}
                      </Title>
                      <TextInput
                        label={t("Название")}
                        value={source.name}
                        onChange={(event) =>
                          setSource({
                            ...source,
                            name: event.currentTarget.value,
                          })
                        }
                      />
                      <TextInput
                        label="ICS URL"
                        description={t(
                          "Можно оставить пустым для загрузки файлов или push.",
                        )}
                        value={source.url}
                        onChange={(event) =>
                          setSource({
                            ...source,
                            url: event.currentTarget.value,
                          })
                        }
                      />
                      <SimpleGrid
                        cols={{ base: 1, sm: 2 }}
                        style={{ alignItems: "end" }}
                      >
                        <NumberInput
                          label={t("Pull: интервал, минуты")}
                          description={t(
                            "0 — автоматическое обновление отключено",
                          )}
                          min={0}
                          value={source.interval / 60}
                          onChange={(value) =>
                            setSource({
                              ...source,
                              interval: Number(value) * 60,
                            })
                          }
                        />
                        <Timezone
                          value={source.timezone}
                          onChange={(timezone) =>
                            setSource({ ...source, timezone })
                          }
                        />
                      </SimpleGrid>
                      <MultiSelect
                        searchable
                        label={t("Тэги новых событий источника")}
                        data={availableTags.filter(
                          (tag) => !tag.startsWith("ics:"),
                        )}
                        value={source.tags}
                        onChange={(tags) => setSource({ ...source, tags })}
                      />
                      <Group justify="space-between">
                        <Button
                          loading={working}
                          onClick={() =>
                            run(async () => {
                              await api(
                                `/api/sources${source.id ? `/${source.id}` : ""}`,
                                source.id ? "PUT" : "POST",
                                source,
                              );
                              setSource(undefined);
                            })
                          }
                        >
                          {t("Сохранить источник")}
                        </Button>
                        {source.id && (
                          <Button
                            color="red"
                            variant="subtle"
                            onClick={() => {
                              if (
                                window.confirm(
                                  t("Удалить источник и его события?"),
                                )
                              )
                                run(async () => {
                                  await api(
                                    `/api/sources/${source.id}`,
                                    "DELETE",
                                  );
                                  setSource(undefined);
                                });
                            }}
                          >
                            {t("Удалить")}
                          </Button>
                        )}
                      </Group>
                      {source.id && (
                        <>
                          <Divider label={t("Push из скрипта")} />
                          <Group>
                            <Badge color={source.hasToken ? "teal" : "gray"}>
                              {source.hasToken
                                ? t("Токен активен")
                                : t("Токена нет")}
                            </Badge>
                            <Button
                              variant="light"
                              size="sm"
                              onClick={() =>
                                run(async () => {
                                  const result = await api<{ token: string }>(
                                    `/api/sources/${source.id}/token`,
                                    "POST",
                                  );
                                  setToken({
                                    id: source.id!,
                                    value: result.token,
                                  });
                                  setSource({ ...source, hasToken: true });
                                })
                              }
                            >
                              {t("Выдать / заменить токен")}
                            </Button>
                            <Button
                              variant="subtle"
                              color="red"
                              size="sm"
                              onClick={() =>
                                run(async () => {
                                  await api(
                                    `/api/sources/${source.id}/token`,
                                    "DELETE",
                                  );
                                  setToken(undefined);
                                  setSource({ ...source, hasToken: false });
                                })
                              }
                            >
                              {t("Отозвать")}
                            </Button>
                          </Group>
                          {token?.id === source.id && (
                            <>
                              <Text size="sm">
                                {t(
                                  "Скопируйте токен сейчас — повторно он не показывается.",
                                )}
                              </Text>
                              <Code block>{token.value}</Code>
                            </>
                          )}
                          <Code
                            block
                          >{`curl -H "Authorization: Bearer $NAC_TOKEN" \\\n  -H 'Content-Type: text/calendar' --data-binary @events.ics \\\n  '${location.origin}/api/sources/${source.id}/import'`}</Code>
                        </>
                      )}
                    </Stack>
                  </Paper>
                )}
              </Stack>
            </Tabs.Panel>
            <Tabs.Panel value="exports">
              <Stack>
                <Text size="sm" c="dimmed">
                  {t(
                    "Любой обладатель ссылки видит выбранные события и поля. Отзыв закрывает ICS и киоск одновременно.",
                  )}
                </Text>
                {state.exports.map((item) => (
                  <Paper withBorder p="md" key={item.id}>
                    <Group justify="space-between">
                      <Text fw={600}>{item.name}</Text>
                      <Group gap="xs">
                        <Anchor
                          href={`/export.ics?id=${item.id}`}
                          target="_blank"
                        >
                          ICS
                        </Anchor>
                        <Button
                          size="compact-sm"
                          variant="light"
                          onClick={() => setKiosk(structuredClone(item))}
                        >
                          {t("Режим киоска")}
                        </Button>
                        <Button
                          size="compact-sm"
                          variant="default"
                          onClick={() =>
                            setFeed((current) =>
                              current?.id === item.id
                                ? undefined
                                : structuredClone(item),
                            )
                          }
                        >
                          {t("Настроить")}
                        </Button>
                      </Group>
                    </Group>
                  </Paper>
                ))}
                <Group>
                  <Button variant="light" onClick={() => newFeed()}>
                    {t("+ Экспорт")}
                  </Button>
                </Group>
                {feed && (
                  <Paper withBorder p="md">
                    <Stack>
                      <TextInput
                        label={t("Название экспорта")}
                        value={feed.name}
                        onChange={(event) =>
                          setFeed({ ...feed, name: event.currentTarget.value })
                        }
                      />
                      <Textarea
                        label={t("Условие отбора (YAML)")}
                        description={t(
                          "true — все события. Условия: tag, not, and, or — как в общих настройках.",
                        )}
                        value={feed.condition}
                        onChange={(event) =>
                          setFeed({
                            ...feed,
                            condition: event.currentTarget.value,
                          })
                        }
                        autosize
                        minRows={4}
                        spellCheck={false}
                        styles={{ input: { fontFamily: "monospace" } }}
                      />
                      <MultiSelect
                        label={t("Какие поля отдавать")}
                        data={fields()}
                        value={feed.fields}
                        description={t(
                          "Время и технические поля ICS сохраняются всегда. Название необязательно.",
                        )}
                        onChange={(fields) => setFeed({ ...feed, fields })}
                      />
                      <Button
                        loading={working}
                        onClick={() =>
                          run(async () => {
                            await api<Export>(
                              `/api/exports${feed.id ? `/${feed.id}` : ""}`,
                              feed.id ? "PUT" : "POST",
                              { ...feed, rule: undefined },
                            );
                            setFeed(undefined);
                          })
                        }
                      >
                        {t("Сохранить экспорт")}
                      </Button>
                      {feed.id && (
                        <>
                          <TextInput
                            label="ICS URL"
                            readOnly
                            value={`${location.origin}/export.ics?id=${feed.id}`}
                          />
                          <Button
                            variant="light"
                            onClick={() => setKiosk(structuredClone(feed))}
                          >
                            {t("Режим киоска")}
                          </Button>
                          <Group>
                            <Button
                              variant="default"
                              onClick={() => {
                                if (
                                  window.confirm(
                                    t(
                                      "Заменить ссылку? Старая перестанет работать.",
                                    ),
                                  )
                                )
                                  run(async () => {
                                    setFeed(
                                      await api<Export>(
                                        `/api/exports/${feed.id}/rotate`,
                                        "POST",
                                      ),
                                    );
                                  });
                              }}
                            >
                              {t("Заменить ссылку")}
                            </Button>
                            <Button
                              variant="subtle"
                              color="red"
                              onClick={() => {
                                if (
                                  window.confirm(
                                    t("Отозвать ссылку и удалить экспорт?"),
                                  )
                                )
                                  run(async () => {
                                    await api(
                                      `/api/exports/${feed.id}`,
                                      "DELETE",
                                    );
                                    setFeed(undefined);
                                  });
                              }}
                            >
                              {t("Отозвать экспорт")}
                            </Button>
                          </Group>
                        </>
                      )}
                    </Stack>
                  </Paper>
                )}
              </Stack>
            </Tabs.Panel>
          </Tabs>
        </Stack>
      </Modal>
      <Modal
        opened={!!kiosk}
        onClose={() => setKiosk(undefined)}
        title={t("Режим киоска")}
        size="lg"
      >
        {kiosk && (
          <Stack>
            {error && <Alert color="red">{error}</Alert>}
            <SimpleGrid cols={{ base: 1, sm: 2 }} style={{ alignItems: "end" }}>
              <Select
                label={t("Вид")}
                data={views()}
                value={kiosk.view}
                allowDeselect={false}
                onChange={(view) =>
                  setKiosk({ ...kiosk, view: view as Export["view"] })
                }
              />
              <Select
                label={t("Тема")}
                data={[
                  { value: "auto", label: t("Системная") },
                  { value: "light", label: t("Светлая") },
                  { value: "dark", label: t("Тёмная") },
                ]}
                value={kiosk.theme}
                allowDeselect={false}
                onChange={(theme) =>
                  setKiosk({ ...kiosk, theme: theme as Export["theme"] })
                }
              />
            </SimpleGrid>
            <SimpleGrid cols={{ base: 1, sm: 2 }} style={{ alignItems: "end" }}>
              <Timezone
                value={kiosk.timezone}
                onChange={(timezone) => setKiosk({ ...kiosk, timezone })}
              />
              <NumberInput
                label={t("Обновление, секунды")}
                min={5}
                max={3600}
                value={kiosk.poll}
                onChange={(value) =>
                  setKiosk({ ...kiosk, poll: Number(value) })
                }
              />
            </SimpleGrid>

            <TextInput
              label={t("Ссылка на киоск")}
              readOnly
              value={`${location.origin}/view?id=${kiosk.id}`}
            />
            <Code
              block
            >{`<iframe src="${location.origin}/view?id=${kiosk.id}" width="100%" height="800" style="border:0"></iframe>`}</Code>
            <Group justify="space-between">
              <Anchor href={`/view?id=${kiosk.id}`} target="_blank">
                {t("Открыть киоск")}
              </Anchor>
              <Button
                loading={working}
                onClick={() =>
                  run(async () => {
                    await api(`/api/exports/${kiosk.id}/kiosk`, "PUT", {
                      view: kiosk.view,
                      theme: kiosk.theme,
                      timezone: kiosk.timezone,
                      poll: kiosk.poll,
                    });
                    setFeed((current) =>
                      current && current.id === kiosk.id
                        ? {
                            ...current,
                            view: kiosk.view,
                            theme: kiosk.theme,
                            timezone: kiosk.timezone,
                            poll: kiosk.poll,
                          }
                        : current,
                    );
                    setKiosk(undefined);
                  })
                }
              >
                {t("Сохранить")}
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </>
  );
}
