import { DateInput, DatePicker } from "@mantine/dates";
import { useEffect, useState } from "react";
import {
  ActionIcon,
  Switch,
  Badge,
  Popover,
  Alert,
  Button,
  Checkbox,
  Group,
  Modal,
  MultiSelect,
  Paper,
  Stack,
  Text,
  Textarea,
  TextInput,
  UnstyledButton,
} from "@mantine/core";
import { api, dayjs, message, type Task } from "./api";
import { t } from "./i18n";

export function useTaskVisibility() {
  const [showCompleted, setShowCompleted] = useState(false);
  const [recentlyCompleted, setRecentlyCompleted] = useState<
    Record<string, number>
  >({});
  useEffect(() => {
    const expirations = Object.values(recentlyCompleted);
    if (!expirations.length) return;
    const timer = setTimeout(
      () => {
        setRecentlyCompleted((current) =>
          Object.fromEntries(
            Object.entries(current).filter(
              ([, expires]) => expires > Date.now(),
            ),
          ),
        );
      },
      Math.max(0, Math.min(...expirations) - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [recentlyCompleted]);

  function completed(task?: Task) {
    if (!task?.id) return;
    setRecentlyCompleted((current) => {
      const next = { ...current };
      if (task.completed) next[task.id] = Date.now() + 10 * 60 * 1000;
      else delete next[task.id];
      return next;
    });
  }
  return { showCompleted, setShowCompleted, recentlyCompleted, completed };
}

export function Tasks({
  tasks,
  tags,
  zone,
  selected,
  onSelect,
  onSaved,
  visibility,
}: {
  visibility: ReturnType<typeof useTaskVisibility>;
  tasks: Task[];
  tags: string[];
  zone: string;
  selected?: Task;
  onSelect: (task?: Task) => void;
  onSaved: () => void;
}) {
  const [title, setTitle] = useState("");
  const [due, setDue] = useState<string | null>(null);
  const [dateOpen, setDateOpen] = useState(false);
  const { showCompleted, setShowCompleted, recentlyCompleted } = visibility;
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const today = dayjs().tz(zone).format("YYYY-MM-DD");
  const visible = tasks.filter(
    (task) => !task.completed || showCompleted || recentlyCompleted[task.id],
  );

  function saved(task?: Task) {
    visibility.completed(task);
    onSaved();
  }

  async function save(task: Task) {
    setSaving(true);
    setError("");
    try {
      await api(
        task.id ? `/api/tasks/${task.id}` : "/api/tasks",
        task.id ? "PUT" : "POST",
        task,
      );
      if (!task.id) {
        setTitle("");
        setDue(null);
      }
      saved(task);
    } catch (error) {
      setError(message(error));
    } finally {
      setSaving(false);
    }
  }

  function row(task: Task) {
    return (
      <Group key={task.id} gap="xs" wrap="nowrap" align="center">
        <Checkbox
          checked={task.completed}
          disabled={saving}
          aria-label={t("Выполнено: {{title}}", { title: task.title })}
          onChange={(event) =>
            void save({ ...task, completed: event.currentTarget.checked })
          }
        />
        <UnstyledButton
          component="div"
          role="button"
          tabIndex={0}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              onSelect(task);
            }
          }}
          style={{
            flex: 1,
            minWidth: 0,
            cursor: task.completed ? "pointer" : "grab",
          }}
          draggable={!task.completed}
          onDragStart={(event) => {
            event.dataTransfer.setData("application/x-nac-task", task.id);
            event.dataTransfer.setData("text/plain", task.title);
            event.dataTransfer.effectAllowed = "copy";
          }}
          onClick={() => onSelect(task)}
        >
          <Text
            size="sm"
            td={task.completed ? "line-through" : undefined}
            style={{ overflowWrap: "anywhere" }}
          >
            {task.title}
          </Text>
          {task.due && (
            <Text
              size="xs"
              c={!task.completed && task.due < today ? "red" : "dimmed"}
            >
              {t("До {{date}}", { date: dayjs(task.due).format("D MMM YYYY") })}
            </Text>
          )}
        </UnstyledButton>
      </Group>
    );
  }

  return (
    <>
      <Paper withBorder radius="lg" p="md">
        <Stack gap="sm">
          <Group justify="space-between">
            <Text fw={600}>{t("Задачи")}</Text>
            <Badge variant="light">
              {tasks.filter((task) => !task.completed).length}
            </Badge>
          </Group>
          {error && <Alert color="red">{error}</Alert>}
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (title.trim() && !saving)
                void save({
                  id: "",
                  version: 0,
                  title,
                  description: "",
                  due: due ?? "",
                  completed: false,
                  tags: [],
                });
            }}
          >
            <Group gap="xs" wrap="nowrap" align="center">
              <TextInput
                style={{ flex: 1, minWidth: 0 }}
                placeholder={t("Добавить задачу…")}
                aria-label={t("Добавить задачу")}
                value={title}
                disabled={saving}
                onChange={(event) => setTitle(event.currentTarget.value)}
              />
              <Popover
                opened={dateOpen}
                onChange={setDateOpen}
                position="bottom-end"
                withArrow
              >
                <Popover.Target>
                  <ActionIcon
                    size={36}
                    variant={due ? "light" : "subtle"}
                    color={due ? undefined : "gray"}
                    disabled={saving}
                    aria-label={t("Срок выполнения")}
                    title={due ?? t("Срок выполнения")}
                    onClick={() => setDateOpen(!dateOpen)}
                  >
                    <svg
                      width="20"
                      height="20"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.6"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      aria-hidden="true"
                    >
                      <rect x="3" y="5" width="18" height="16" rx="2" />
                      <path d="M16 3v4M8 3v4M3 11h18" />
                    </svg>
                  </ActionIcon>
                </Popover.Target>
                <Popover.Dropdown>
                  <DatePicker
                    value={due}
                    onChange={(value) => {
                      setDue(value);
                      setDateOpen(false);
                    }}
                  />
                  {due && (
                    <Button
                      fullWidth
                      variant="subtle"
                      size="xs"
                      mt="xs"
                      onClick={() => {
                        setDue(null);
                        setDateOpen(false);
                      }}
                    >
                      {t("Без срока")}
                    </Button>
                  )}
                </Popover.Dropdown>
              </Popover>
            </Group>
            {due && (
              <Text size="xs" c="dimmed" mt={4}>
                {t("До {{date}}", { date: dayjs(due).format("D MMM YYYY") })}
              </Text>
            )}
          </form>
          {!!visible.length && (
            <Stack gap="sm" mah={360} style={{ overflowY: "auto" }}>
              {visible.map(row)}
            </Stack>
          )}
          <Switch
            label={t("Выполненные")}
            labelPosition="right"
            styles={{
              label: {
                fontSize: "var(--mantine-font-size-xs)",
                color: "var(--mantine-color-dimmed)",
              },
            }}
            checked={showCompleted}
            onChange={(event) => setShowCompleted(event.currentTarget.checked)}
          />
        </Stack>
      </Paper>
      {selected && (
        <TaskEditor
          key={selected.id}
          task={selected}
          tags={tags}
          onClose={() => onSelect(undefined)}
          onSaved={saved}
        />
      )}
    </>
  );
}

function TaskEditor({
  task,
  tags,
  onClose,
  onSaved,
}: {
  task: Task;
  tags: string[];
  onClose: () => void;
  onSaved: (task?: Task) => void;
}) {
  const [form, setForm] = useState(task);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  async function submit(remove = false) {
    if (
      remove &&
      !confirm(t("Удалить задачу? Рабочие интервалы останутся в календаре."))
    )
      return;
    setSaving(true);
    try {
      await api(
        `/api/tasks/${task.id}${remove ? `?version=${form.version}` : ""}`,
        remove ? "DELETE" : "PUT",
        remove ? undefined : form,
      );
      onSaved(remove ? undefined : form);
      onClose();
    } catch (error) {
      setError(message(error));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal opened onClose={onClose} title={t("Задача")}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <Stack>
          {error && <Alert color="red">{error}</Alert>}
          <TextInput
            label={t("Название")}
            required
            maxLength={1000}
            value={form.title}
            onChange={(event) =>
              setForm({ ...form, title: event.currentTarget.value })
            }
          />
          <Textarea
            label={t("Описание")}
            autosize
            minRows={3}
            value={form.description}
            onChange={(event) =>
              setForm({ ...form, description: event.currentTarget.value })
            }
          />
          <DateInput
            clearable
            valueFormat="YYYY-MM-DD"
            label={t("Срок выполнения")}
            description={t(
              "Необязательно. Время работы задаётся отдельно в календаре.",
            )}
            value={form.due || null}
            onChange={(due) => setForm({ ...form, due: due ?? "" })}
          />
          <MultiSelect
            label={t("Мои тэги")}
            data={[
              ...new Set([
                ...tags.filter((tag) => !tag.startsWith("ics:")),
                ...form.tags,
              ]),
            ]}
            value={form.tags}
            onChange={(tags) => setForm({ ...form, tags })}
            searchable
          />
          <Checkbox
            label={t("Выполнено")}
            checked={form.completed}
            onChange={(event) =>
              setForm({ ...form, completed: event.currentTarget.checked })
            }
          />
          <Group justify="space-between">
            <Button
              color="red"
              variant="subtle"
              disabled={saving}
              onClick={() => void submit(true)}
            >
              {t("Удалить")}
            </Button>
            <Button type="submit" loading={saving}>
              {t("Сохранить")}
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
