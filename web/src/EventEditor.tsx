import i18n, { t, scheduleLabels } from "./i18n";
import { useCallback, useEffect, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Container,
  Group,
  Paper,
  MultiSelect,
  SegmentedControl,
  Select,
  SimpleGrid,
  Stack,
  Text,
  Textarea,
  TextInput,
  Title,
} from "@mantine/core";
import { usePolling } from "./usePolling";
import { ResourcesSchedule, type ScheduleEventData } from "@mantine/schedule";
import {
  api,
  dateRange,
  dayjs,
  displayTime,
  effectiveTimezone,
  message,
  toInstant,
  allDayRange,
  eventTimes,
  moveToSlot as movedToSlot,
  type Event,
  type EventDetail,
  type Occurrence,
  type State,
} from "./api";

export function EventEditor({
  state,
  occurrence,
  onClose,
  onSaved,
}: {
  state: State;
  occurrence: Occurrence;
  onClose: () => void;
  onSaved: () => void;
}) {
  const zone = effectiveTimezone(state.me.settings.timezone);
  const [detail, setDetail] = useState<EventDetail>();
  const [scope, setScope] = useState(occurrence.rid ? "occurrence" : "series");
  const [form, setForm] = useState<Event>();
  const [availability, setAvailability] = useState<
    Record<string, Occurrence[]>
  >({});
  const [availabilityError, setAvailabilityError] = useState("");
  const [scheduleDate, setScheduleDate] = useState(
    dayjs(occurrence.start).tz(zone).format("YYYY-MM-DD"),
  );
  const [scheduleView, setScheduleView] =
    useState<ResourcesSchedule.ViewLevel>("day");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const isNew = !occurrence.eventId;
  const editable = isNew || !!detail?.editable;
  const rid = scope === "occurrence" ? occurrence.rid : "";

  useEffect(() => {
    let active = true;
    if (isNew) {
      setForm({
        id: "",
        creator: state.me.id,
        editPolicy: "all",
        version: 0,
        title: "",
        start: occurrence.start,
        end: occurrence.end,
        timezone: zone,
        allDay: occurrence.allDay,
        description: "",
        location: "",
        url: "",
        cancelled: false,
        rrule: "",
        members: [{ user: state.me.id }],
      });
    } else {
      api<EventDetail>(`/api/events/${occurrence.eventId}`)
        .then((value) => {
          if (active) setDetail(value);
        })
        .catch((error) => {
          if (active) setError(message(error));
        });
    }
    return () => {
      active = false;
    };
  }, [occurrence.eventId]);

  useEffect(() => {
    if (!detail) return;
    const master = detail.event;
    const selected = rid
      ? (master.overrides?.[rid] ?? {
          ...master,
          start: occurrence.start,
          end: occurrence.end,
        })
      : master;
    setForm({
      ...selected,
      creator: master.creator,
      editPolicy: master.editPolicy,
      members: master.members,
      version: master.version,
      rrule: master.rrule,
    });
    setScheduleDate(dayjs(selected.start).tz(zone).format("YYYY-MM-DD"));
  }, [detail, rid]);

  const memberIDs = form?.members.map((member) => member.user) ?? [];
  const membersKey = JSON.stringify(memberIDs);
  const loadAvailability = useCallback(
    async (signal: AbortSignal) => {
      try {
        const query = dateRange(scheduleDate, zone, scheduleView);
        const result = await api<Record<string, Occurrence[]>>(
          `/api/availability?${query}`,
          "POST",
          JSON.parse(membersKey),
          signal,
        );
        if (!signal.aborted) {
          setAvailability(result);
          setAvailabilityError("");
        }
      } catch (error) {
        if (!signal.aborted) setAvailabilityError(message(error));
      }
    },
    [membersKey, scheduleDate, scheduleView, zone],
  );
  usePolling(loadAvailability, state.me.settings.poll, !!form);

  function patch(update: Partial<Event>) {
    setForm((current) => (current ? { ...current, ...update } : current));
    if (update.start)
      setScheduleDate(dayjs(update.start).tz(zone).format("YYYY-MM-DD"));
  }
  function canMove(event: ScheduleEventData) {
    return editable && !saving && event.payload?.draft === true;
  }
  function moveToSlot(value: string) {
    if (!form || !editable || saving) return;
    patch(movedToSlot(form, value, zone));
  }
  function moveDraft({
    event,
    newStart,
    newEnd,
  }: {
    event: ScheduleEventData;
    newStart: string;
    newEnd: string;
  }) {
    if (!form || !canMove(event)) return;
    patch(
      eventTimes(
        newStart,
        newEnd,
        form.allDay,
        form.allDay ? form.timezone : zone,
      ),
    );
  }

  async function save() {
    if (!form) return;
    setSaving(true);
    setError("");
    try {
      const { overrides: _, ...body } = form;
      const users = [...new Set([form.creator, ...memberIDs])];
      await api(
        `/api/events${isNew ? "" : `/${form.id}`}${rid ? `?rid=${encodeURIComponent(rid)}` : ""}`,
        isNew ? "POST" : "PUT",
        {
          ...body,
          members: users.map((user) => ({
            user,
          })),
        },
      );
      onSaved();
      onClose();
    } catch (error) {
      setError(message(error));
    } finally {
      setSaving(false);
    }
  }
  async function remove() {
    if (
      !form ||
      !window.confirm(
        rid
          ? t("Удалить это вхождение?")
          : t("Удалить событие и все его повторения?"),
      )
    )
      return;
    setSaving(true);
    try {
      const query = new URLSearchParams({
        version: String(form.version),
        ...(rid ? { rid } : {}),
      });
      await api(`/api/events/${form.id}?${query}`, "DELETE");
      onSaved();
      onClose();
    } catch (error) {
      setError(message(error));
    } finally {
      setSaving(false);
    }
  }

  const resources = memberIDs.map((id) => {
    const user = state.users.find((user) => user.id === id);
    return { id, label: user?.name || user?.login || id };
  });
  const scheduleEvents: ScheduleEventData[] = resources.flatMap((resource) => [
    ...(availability[resource.id] ?? [])
      .filter(
        (event) =>
          event.busy &&
          !(
            event.eventId === occurrence.eventId &&
            (!rid || event.rid === rid)
          ),
      )
      .map((event) => ({
        id: `${resource.id}/${event.id}`,
        resourceId: resource.id,
        title: event.title || t("Занят"),
        color: "gray",
        start: displayTime(
          event.start,
          event.allDay ? (event.timezone ?? zone) : zone,
        ),
        end: displayTime(
          event.end,
          event.allDay ? (event.timezone ?? zone) : zone,
        ),
      })),
    ...(form
      ? [
          {
            id: `draft/${resource.id}`,
            resourceId: resource.id,
            payload: { draft: true },
            title: form.title,
            color: "teal",
            variant: "light" as const,
            start: displayTime(form.start, form.allDay ? form.timezone : zone),
            end: displayTime(form.end, form.allDay ? form.timezone : zone),
          },
        ]
      : []),
  ]);

  return (
    <Container fluid p="lg" className="app">
      <Stack>
        <Group>
          <Button variant="subtle" onClick={onClose}>
            {t("← К календарю")}
          </Button>
        </Group>
        {error && <Alert color="red">{error}</Alert>}
        {!form && !error && <Text c="dimmed">{t("Загрузка…")}</Text>}
        {form && (
          <div className="event-page">
            <Stack>
              {occurrence.rid && (
                <SegmentedControl
                  value={scope}
                  onChange={setScope}
                  data={[
                    { value: "occurrence", label: t("Это вхождение") },
                    { value: "series", label: t("Вся серия") },
                  ]}
                />
              )}
              {form.source && (
                <Text size="sm" c="dimmed">
                  {t("Импортировано. Содержимое меняется в источнике.")}
                </Text>
              )}
              <fieldset
                disabled={!editable || saving}
                className="plain-fieldset"
              >
                <Stack>
                  <TextInput
                    label={t("Название")}
                    placeholder={t("На что выделим время?")}
                    value={form.title}
                    onChange={(event) =>
                      patch({ title: event.currentTarget.value })
                    }
                    autoFocus={isNew}
                  />
                  <Stack gap="xs">
                    <SimpleGrid cols={2}>
                      {(["start", "end"] as const).map((field) => (
                        <TextInput
                          key={`${field}-${form.allDay}`}
                          label={
                            field === "start"
                              ? t("Начало")
                              : t("Конец (не включительно)")
                          }
                          type={form.allDay ? "date" : "datetime-local"}
                          value={dayjs(form[field])
                            .tz(form.allDay ? form.timezone : zone)
                            .format(
                              form.allDay ? "YYYY-MM-DD" : "YYYY-MM-DDTHH:mm",
                            )}
                          onChange={(event) => {
                            if (event.currentTarget.value)
                              patch({
                                [field]: toInstant(
                                  event.currentTarget.value,
                                  form.allDay ? form.timezone : zone,
                                ),
                              });
                          }}
                        />
                      ))}
                    </SimpleGrid>
                    <Checkbox
                      label={t("Весь день")}
                      checked={form.allDay}
                      onChange={(event) => {
                        const allDay = event.currentTarget.checked;
                        const date = dayjs(form.start)
                          .tz(zone)
                          .format("YYYY-MM-DD");
                        patch(
                          allDay
                            ? {
                                allDay,
                                timezone: zone,
                                ...allDayRange(date, 1, zone),
                              }
                            : { allDay },
                        );
                      }}
                    />
                  </Stack>
                  <MultiSelect
                    label={t("Участники")}
                    placeholder={t("Найти и добавить человека")}
                    searchable
                    hidePickedOptions
                    limit={20}
                    maxDropdownHeight={240}
                    nothingFoundMessage={t("Никого не найдено")}
                    data={state.users.map((user) => ({
                      value: user.id,
                      label: user.name || user.login,
                      disabled: user.id === form.creator,
                    }))}
                    value={memberIDs}
                    onChange={(users) =>
                      patch({
                        members: [...new Set([form.creator, ...users])].map(
                          (user) => ({
                            user,
                          }),
                        ),
                      })
                    }
                    styles={{ input: { maxHeight: 160, overflowY: "auto" } }}
                  />
                  <Select
                    label={t("Кто может редактировать")}
                    value={form.editPolicy}
                    allowDeselect={false}
                    data={[
                      { value: "all", label: t("Все участники") },
                      { value: "author", label: t("Только автор") },
                    ]}
                    onChange={(value) =>
                      patch({ editPolicy: value as "all" | "author" })
                    }
                  />
                  <Select
                    label={t("Повторение")}
                    disabled={scope === "occurrence"}
                    data={[
                      { value: "", label: t("Не повторять") },
                      { value: "FREQ=DAILY", label: t("Каждый день") },
                      { value: "FREQ=WEEKLY", label: t("Каждую неделю") },
                      { value: "FREQ=MONTHLY", label: t("Каждый месяц") },
                      { value: "FREQ=YEARLY", label: t("Каждый год") },
                      { value: "custom", label: t("Другое правило") },
                    ]}
                    value={
                      [
                        "",
                        "FREQ=DAILY",
                        "FREQ=WEEKLY",
                        "FREQ=MONTHLY",
                        "FREQ=YEARLY",
                      ].includes(form.rrule)
                        ? form.rrule
                        : "custom"
                    }
                    onChange={(rule) =>
                      patch({
                        rrule:
                          rule === "custom"
                            ? "FREQ=WEEKLY;BYDAY=MO,WE,FR"
                            : (rule ?? ""),
                      })
                    }
                  />
                  {form.rrule && scope !== "occurrence" && (
                    <TextInput
                      label="RRULE"
                      value={form.rrule}
                      onChange={(event) =>
                        patch({ rrule: event.currentTarget.value })
                      }
                    />
                  )}
                  <Textarea
                    label={t("Описание")}
                    autosize
                    minRows={2}
                    value={form.description}
                    onChange={(event) =>
                      patch({ description: event.currentTarget.value })
                    }
                  />
                  <TextInput
                    label={t("Место")}
                    value={form.location}
                    onChange={(event) =>
                      patch({ location: event.currentTarget.value })
                    }
                  />
                  <TextInput
                    label={t("Ссылка")}
                    value={form.url}
                    onChange={(event) =>
                      patch({ url: event.currentTarget.value })
                    }
                  />
                </Stack>
              </fieldset>
              <Group justify="space-between">
                {editable && !isNew ? (
                  <Button
                    color="red"
                    variant="subtle"
                    onClick={remove}
                    disabled={saving}
                  >
                    {t("Удалить")}
                  </Button>
                ) : (
                  <span />
                )}
                {editable && (
                  <Button onClick={save} loading={saving}>
                    {isNew ? t("Создать событие") : t("Сохранить событие")}
                  </Button>
                )}
              </Group>
            </Stack>
            <Stack className="participants-schedule">
              <Title order={3}>{t("Расписание участников")}</Title>
              {availabilityError && (
                <Alert color="red">{availabilityError}</Alert>
              )}
              <Paper withBorder className="calendar">
                <ResourcesSchedule
                  mode={editable ? "default" : "static"}
                  withEventsDragAndDrop={editable && !saving}
                  withEventResize={editable && !saving}
                  canDragEvent={canMove}
                  canResizeEvent={canMove}
                  onEventDrop={moveDraft}
                  onEventResize={moveDraft}
                  onTimeSlotClick={({ slotStart }) => moveToSlot(slotStart)}
                  onDayClick={({ date }) => moveToSlot(date)}
                  resources={resources}
                  events={scheduleEvents}
                  date={scheduleDate}
                  onDateChange={setScheduleDate}
                  view={scheduleView}
                  onViewChange={setScheduleView}
                  locale={i18n.language}
                  labels={scheduleLabels()}
                  dayViewProps={{
                    classNames: { header: "calendar-header" },
                    startScrollTime: dayjs(form.start)
                      .tz(zone)
                      .format("HH:mm:ss"),
                    getCurrentTime: () =>
                      dayjs().tz(zone).format("YYYY-MM-DD HH:mm:ss"),
                    scrollAreaProps: { mah: "65vh" },
                  }}
                  weekViewProps={{
                    classNames: { header: "calendar-header" },
                    scrollAreaProps: { mah: "65vh" },
                  }}
                  monthViewProps={{
                    classNames: { header: "calendar-header" },
                    scrollAreaProps: { mah: "65vh" },
                  }}
                />
              </Paper>
            </Stack>
          </div>
        )}
      </Stack>
    </Container>
  );
}
