import { useTranslation } from "react-i18next";
import i18n, { t, scheduleLabels } from "./i18n";
import { useCallback, useEffect, useState } from "react";
import {
  Alert,
  ActionIcon,
  useMantineTheme,
  Badge,
  Box,
  UnstyledButton,
  Button,
  Menu,
  Center,
  Container,
  Group,
  Loader,
  Modal,
  Paper,
  Stack,
  Text,
  useMantineColorScheme,
} from "@mantine/core";
import {
  Schedule,
  MobileMonthView,
  AgendaView,
  type ScheduleViewLevel,
  type ScheduleEventData,
  type ScheduleEventProps,
} from "@mantine/schedule";
import {
  APIError,
  api,
  dateRange,
  dayjs,
  displayTime,
  effectiveTimezone,
  eventTimes,
  message,
  toInstant,
  type EventDetail,
  type Occurrence,
  type PublicView,
  type State,
  type Task,
} from "./api";
import { EventEditor } from "./EventEditor";
import { EventPopover } from "./EventPopover";
import { Tasks, useTaskVisibility } from "./Tasks";
import { usePolling } from "./usePolling";
import { SettingsPanel } from "./SettingsPanel";

function mantineColor(color: string) {
  return color === "grey" ? "gray" : color;
}

function scheduleEvent(event: Occurrence, zone: string): ScheduleEventData {
  return {
    id: event.id,
    title: event.title ?? "",
    start: displayTime(
      event.start,
      event.allDay ? (event.timezone ?? zone) : zone,
    ),
    end: displayTime(event.end, event.allDay ? (event.timezone ?? zone) : zone),
    color: mantineColor(event.color),
    variant: "light",
    payload: { event },
  };
}

function Calendar({
  events,
  tasks = [],
  onSelectTask,
  date,
  view,
  zone,
  onDate,
  onView,
  onSelect,
  onMove,
  onCreate,
  onTaskDrop,
}: {
  onTaskDrop?: (data: DataTransfer, start: string) => void;
  tasks?: Task[];
  onSelectTask?: (task: Task) => void;
  events: Occurrence[];
  date: string;
  view: string;
  zone: string;
  onDate: (date: string) => void;
  onView?: (view: ScheduleViewLevel) => void;
  onSelect: (event: Occurrence, anchor: HTMLElement) => void;
  onMove?: (event: Occurrence, start: string, end: string) => void;
  onCreate?: (start: string, end: string, allDay?: boolean) => void;
}) {
  const theme = useMantineTheme();
  const renderEvent: NonNullable<ScheduleEventProps["renderEvent"]> = (
    event,
    props,
  ) => {
    const item = event.payload?.event ?? event.payload?.task;
    const accent = item.stripe
      ? theme.variantColorResolver({
          theme,
          color: mantineColor(item.stripe),
          variant: "filled",
        }).background
      : undefined;
    return (
      <UnstyledButton
        {...props}
        data-accent={accent ? true : undefined}
        style={{
          ...props.style,
          "--nac-event-accent": accent,
          textDecoration: item.completed ? "line-through" : undefined,
        }}
      />
    );
  };
  const data = events.map((event) => scheduleEvent(event, zone));
  for (const task of tasks) {
    if (!task.due) continue;
    data.push({
      id: `task:${task.id}`,
      title: `${task.completed ? "✓" : "☐"} ${task.title}`,
      start: `${task.due} 00:00:00`,
      end: dayjs(task.due).add(1, "day").format("YYYY-MM-DD 00:00:00"),
      color: mantineColor(task.color ?? "teal"),
      variant: "light",
      payload: { task },
    });
  }
  function select(item: ScheduleEventData, anchor: HTMLElement) {
    if (item.payload?.task) onSelectTask?.(item.payload.task as Task);
    else onSelect(item.payload?.event as Occurrence, anchor);
  }
  function move(data: {
    event: ScheduleEventData;
    newStart: string;
    newEnd: string;
  }) {
    const event = data.event.payload?.event as Occurrence;
    if (event && onMove) onMove(event, data.newStart, data.newEnd);
  }
  const labels = scheduleLabels();

  if (view === "list") {
    return (
      <AgendaView
        rangeStart={dayjs(date).startOf("month").format("YYYY-MM-DD")}
        rangeEnd={dayjs(date).endOf("month").format("YYYY-MM-DD")}
        events={data}
        locale={i18n.language}
        labels={labels}
        renderEvent={renderEvent}
        onEventClick={(event, click) => select(event, click.currentTarget)}
      />
    );
  }
  return (
    <Schedule
      date={date}
      view={view as ScheduleViewLevel}
      onViewChange={onView}
      withAgenda={!!onView}
      onDateChange={onDate}
      events={data}
      locale={i18n.language}
      labels={labels}
      layout="responsive"
      dayViewProps={{
        startScrollTime: "09:00",
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
        styles: { dayView: { "--day-view-slot-labels-width": "7rem" } },
        renderEvent,
        getCurrentTime: () => dayjs().tz(zone).format("YYYY-MM-DD HH:mm:ss"),
      }}
      weekViewProps={{
        startScrollTime: "09:00",
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
        styles: { weekView: { "--week-view-slots-label-width": "7rem" } },
        renderEvent,
        firstDayOfWeek: 1,
        getCurrentTime: () => dayjs().tz(zone).format("YYYY-MM-DD HH:mm:ss"),
      }}
      monthViewProps={{
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
        firstDayOfWeek: 1,
        renderEvent,
      }}
      yearViewProps={{
        withHeader: !!onView,
        classNames: { header: "calendar-header" },
      }}
      mobileMonthViewProps={{
        renderHeader: onView ? undefined : () => null,
        renderEvent,
      }}
      onTimeSlotClick={({ slotStart, slotEnd }) =>
        onCreate?.(slotStart, slotEnd)
      }
      onAllDaySlotClick={(date) =>
        onCreate?.(date, dayjs(date).add(1, "day").format("YYYY-MM-DD"), true)
      }
      onDayClick={(date) => onCreate?.(`${date} 09:00`, `${date} 10:00`)}
      withDragSlotSelect={!!onCreate}
      onSlotDragEnd={(start, end) => onCreate?.(start, end)}
      onEventClick={(event, click) => select(event, click.currentTarget)}
      withEventsDragAndDrop={!!onMove}
      withEventResize={!!onMove}
      canDragEvent={(event) => !!event.payload?.event?.editable}
      canResizeEvent={(event) => !!event.payload?.event?.editable}
      onExternalEventDrop={onTaskDrop}
      onEventDrop={move}
      onEventResize={move}
    />
  );
}

function editorFromURL(): Occurrence | undefined {
  if (location.pathname !== "/event") return undefined;
  const query = new URLSearchParams(location.search);
  const eventId = query.get("id") ?? "";
  const start = query.get("start") ?? new Date().toISOString();
  return {
    id: eventId,
    eventId: eventId || undefined,
    rid: query.get("rid") ?? undefined,
    start,
    end: query.get("end") ?? dayjs(start).add(1, "hour").toISOString(),
    allDay: query.get("allDay") === "true",
    color: "teal",
  };
}

export default function App() {
  useTranslation();
  const shared = location.pathname === "/view";
  const exportID = new URLSearchParams(location.search).get("id") ?? "";
  const [state, setState] = useState<State>();
  const [publicView, setPublicView] = useState<PublicView>();
  const [events, setEvents] = useState<Occurrence[]>([]);
  const [date, setDate] = useState(dayjs().format("YYYY-MM-DD"));
  const [view, setView] = useState("week");
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState("");
  const [editor, setEditor] = useState<Occurrence | undefined>(editorFromURL);
  const [selectedTask, setSelectedTask] = useState<Task>();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [preview, setPreview] = useState<Occurrence>();
  const [selectedEvent, setSelectedEvent] = useState<{
    event: Occurrence;
    anchor: HTMLElement;
  }>();
  const [revision, setRevision] = useState(0);
  const { setColorScheme } = useMantineColorScheme();
  const zone = effectiveTimezone(
    publicView?.timezone ?? state?.me.settings.timezone,
  );
  const refresh = () => setRevision((value) => value + 1);
  const taskVisibility = useTaskVisibility();

  useEffect(() => {
    const restore = () => setEditor(editorFromURL());
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, []);

  useEffect(() => {
    setSelectedEvent(undefined);
  }, [date, view, settingsOpen]);

  function openEditor(event: Occurrence) {
    setSelectedEvent(undefined);
    const query = new URLSearchParams({
      start: event.start,
      end: event.end,
      allDay: String(event.allDay),
    });
    if (event.eventId) query.set("id", event.eventId);
    if (event.rid) query.set("rid", event.rid);
    history.pushState({ eventPage: true }, "", `/event?${query}`);
    setEditor(event);
  }
  function closeEditor() {
    if (history.state?.eventPage) history.back();
    else {
      history.replaceState(null, "", "/");
      setEditor(undefined);
    }
  }

  const load = useCallback(
    async (signal: AbortSignal) => {
      try {
        if (shared) {
          const today = dayjs().tz(zone).format("YYYY-MM-DD");
          const query = dateRange(today, zone, view);
          query.set("id", exportID);
          const data = await api<PublicView>(
            `/view/data?${query}`,
            "GET",
            undefined,
            signal,
          );
          if (signal.aborted) return;
          setPublicView(data);
          setEvents(data.events);
          setPreview((current) =>
            current
              ? data.events.find((event) => event.id === current.id)
              : undefined,
          );
          setDate(today);
          setView(data.view);
          setColorScheme(data.theme);
        } else {
          const data = await api<State>("/api/state", "GET", undefined, signal);
          const query = dateRange(
            date,
            effectiveTimezone(data.me.settings.timezone),
            view,
          );
          const events = await api<Occurrence[]>(
            `/api/events?${query}`,
            "GET",
            undefined,
            signal,
          );
          if (signal.aborted) return;
          setState(data);
          setEvents(events);
        }
        setError("");
        setUpdated(dayjs().format("HH:mm:ss"));
      } catch (error) {
        if (signal.aborted) return;
        setError(message(error));
        if (
          error instanceof APIError &&
          [401, 403, 404].includes(error.status)
        ) {
          setEvents([]);
          setPreview(undefined);
          setEditor(undefined);
          if (shared) setPublicView(undefined);
          else setState(undefined);
        }
      }
    },
    [shared, exportID, date, revision, zone, view],
  );
  usePolling(
    load,
    publicView?.poll ?? state?.me.settings.poll ?? 15,
    !editor || !state,
  );

  async function move(event: Occurrence, start: string, end: string) {
    try {
      const detail = await api<EventDetail>(`/api/events/${event.eventId}`);
      if (detail.event.version !== event.version)
        throw new Error(t("Событие изменилось. Обновите страницу."));
      const master = detail.event;
      const { overrides: _, ...body } = event.rid
        ? (master.overrides?.[event.rid] ?? master)
        : master;
      await api(
        `/api/events/${event.eventId}${event.rid ? `?rid=${encodeURIComponent(event.rid)}` : ""}`,
        "PUT",
        {
          ...body,
          version: master.version,
          members: master.members,
          rrule: master.rrule,
          ...eventTimes(
            start,
            end,
            event.allDay,
            event.allDay ? (event.timezone ?? zone) : zone,
          ),
        },
      );
      refresh();
    } catch (error) {
      setError(message(error));
    }
  }
  async function scheduleTask(data: DataTransfer, start: string) {
    const task = state?.tasks.find(
      (task) => task.id === data.getData("application/x-nac-task"),
    );
    if (!task || !state) return;
    try {
      const instant = toInstant(start, zone);
      await api("/api/events", "POST", {
        taskId: task.id,
        title: task.title,
        description: task.description,
        start: instant,
        end: dayjs(instant).add(1, "hour").toISOString(),
        timezone: zone,
        editPolicy: "author",
        members: [],
      });
      refresh();
    } catch (error) {
      setError(message(error));
    }
  }

  function createEvent(
    start = `${date} 09:00`,
    end = `${date} 10:00`,
    allDay = false,
  ) {
    openEditor({
      id: "",
      start: toInstant(start, zone),
      end: toInstant(end, zone),
      allDay,
      color: "teal",
    });
  }
  function selectEvent(event: Occurrence, anchor: HTMLElement) {
    if (!event) return;
    if (!shared && event.eventId) setSelectedEvent({ event, anchor });
    else setPreview(event);
  }

  if (editor && state) {
    return (
      <EventEditor
        key={editor.id || "new"}
        state={state}
        occurrence={editor}
        onClose={closeEditor}
        onSaved={refresh}
      />
    );
  }

  return (
    <Container
      fluid
      p={shared ? 0 : "lg"}
      className="app calendar-page"
      data-shared={shared || undefined}
    >
      <Stack gap="lg" className="calendar-shell">
        {!shared && (
          <Group justify="space-between" align="center">
            <div className="brand">
              nac<span>●</span>
            </div>
            {state && (
              <Group gap="sm" ml="auto">
                <Button onClick={() => createEvent()}>{t("+ Событие")}</Button>
                <Menu position="bottom-end">
                  <Menu.Target>
                    <Button
                      variant="subtle"
                      color="gray"
                      aria-label={t("Профиль")}
                    >
                      {state.me.name || state.me.login}
                    </Button>
                  </Menu.Target>
                  <Menu.Dropdown>
                    <Menu.Item onClick={() => setSettingsOpen(true)}>
                      {t("Настройки")}
                    </Menu.Item>
                    <Menu.Divider />
                    <Menu.Label>{t("Язык")}</Menu.Label>
                    <Menu.Item onClick={() => void i18n.changeLanguage("ru")}>
                      Русский
                    </Menu.Item>
                    <Menu.Item onClick={() => void i18n.changeLanguage("en")}>
                      English
                    </Menu.Item>
                  </Menu.Dropdown>
                </Menu>
              </Group>
            )}
          </Group>
        )}
        {error && (
          <Alert color="red" title={t("Не удалось обновить данные")}>
            {shared && !publicView
              ? t("Ссылка недоступна или отозвана.")
              : error}
          </Alert>
        )}
        {(!shared && state) || (shared && publicView) ? (
          <div className="calendar-layout" data-shared={shared || undefined}>
            {!shared && (
              <aside className="calendar-sidebar">
                <Paper withBorder radius="lg" className="sidebar-month">
                  <MobileMonthView
                    h="auto"
                    date={date}
                    selectedDate={date}
                    events={events.map((event) => scheduleEvent(event, zone))}
                    onDayClick={setDate}
                    locale={i18n.language}
                    labels={scheduleLabels()}
                    firstDayOfWeek={1}
                    weekdayFormat="dd"
                    withOutsideDays
                    styles={{
                      mobileMonthViewEventsList: { display: "none" },
                    }}
                    renderHeader={() => (
                      <Group justify="space-between" wrap="nowrap" w="100%">
                        <ActionIcon
                          variant="subtle"
                          color="gray"
                          aria-label={t("Предыдущий месяц")}
                          onClick={() =>
                            setDate(
                              dayjs(date)
                                .subtract(1, "month")
                                .format("YYYY-MM-DD"),
                            )
                          }
                        >
                          ‹
                        </ActionIcon>
                        <Text fw={600}>
                          {dayjs(date).format("MMMM YYYY")}
                        </Text>
                        <ActionIcon
                          variant="subtle"
                          color="gray"
                          aria-label={t("Следующий месяц")}
                          onClick={() =>
                            setDate(
                              dayjs(date).add(1, "month").format("YYYY-MM-DD"),
                            )
                          }
                        >
                          ›
                        </ActionIcon>
                      </Group>
                    )}
                  />
                  <Box p="sm">
                    <Button
                      fullWidth
                      variant="subtle"
                      onClick={() =>
                        setDate(dayjs().tz(zone).format("YYYY-MM-DD"))
                      }
                    >
                      {t("Сегодня")}
                    </Button>
                  </Box>
                </Paper>
                {state && (
                  <Tasks
                    tasks={state.tasks}
                    tags={state.tags}
                    zone={zone}
                    selected={selectedTask}
                    onSelect={setSelectedTask}
                    onSaved={refresh}
                    visibility={taskVisibility}
                  />
                )}
              </aside>
            )}
            <div className="calendar">
              <Calendar
                events={events}
                tasks={shared ? undefined : state?.tasks}
                onSelectTask={setSelectedTask}
                date={date}
                view={view}
                zone={zone}
                onDate={setDate}
                onView={shared ? undefined : setView}
                onSelect={selectEvent}
                onMove={shared ? undefined : move}
                onCreate={shared ? undefined : createEvent}
                onTaskDrop={shared ? undefined : scheduleTask}
              />
            </div>
          </div>
        ) : (
          !error && (
            <Center py={100}>
              <Loader />
            </Center>
          )
        )}
        {!shared && (
          <Text size="xs" c="dimmed" ta="right">
            {updated && t("Обновлено {{time}}", { time: updated })}
          </Text>
        )}
      </Stack>
      {selectedEvent && state && (
        <EventPopover
          key={selectedEvent.event.id}
          event={selectedEvent.event}
          anchor={selectedEvent.anchor}
          tags={state.tags}
          zone={zone}
          onClose={() => setSelectedEvent(undefined)}
          onSaved={refresh}
          onOpenTask={
            state.tasks.some((task) => task.id === selectedEvent.event.taskId)
              ? () => {
                  setSelectedTask(
                    state.tasks.find(
                      (task) => task.id === selectedEvent.event.taskId,
                    ),
                  );
                  setSelectedEvent(undefined);
                }
              : undefined
          }
          onEdit={(series) =>
            openEditor({
              ...selectedEvent.event,
              rid: series ? undefined : selectedEvent.event.rid,
            })
          }
        />
      )}
      {settingsOpen && state && (
        <SettingsPanel
          state={state}
          onClose={() => setSettingsOpen(false)}
          onSaved={refresh}
        />
      )}
      <Modal
        opened={!!preview}
        onClose={() => setPreview(undefined)}
        title={preview?.title ?? t("Интервал времени")}
      >
        {preview && (
          <Stack>
            <Text>
              {dayjs(preview.start).tz(zone).format("D MMMM, HH:mm")} —{" "}
              {dayjs(preview.end).tz(zone).format("D MMMM, HH:mm")}
            </Text>
            {preview.description && (
              <Text style={{ whiteSpace: "pre-wrap" }}>
                {preview.description}
              </Text>
            )}
            {preview.location && <Text>{preview.location}</Text>}
            {preview.url && <Text>{preview.url}</Text>}
            {preview.participants?.map((person) => (
              <Text key={person.id}>{person.name}</Text>
            ))}

            <Group>
              {preview.tags?.map((tag) => (
                <Badge key={tag}>{tag}</Badge>
              ))}
            </Group>
          </Stack>
        )}
      </Modal>
    </Container>
  );
}
