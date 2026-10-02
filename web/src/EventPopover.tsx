import { useEffect, useState } from "react";
import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Paper,
  Stack,
  Text,
} from "@mantine/core";
import {
  autoUpdate,
  flip,
  FloatingFocusManager,
  FloatingPortal,
  offset,
  shift,
  useDismiss,
  useFloating,
  useInteractions,
  useRole,
} from "@floating-ui/react";
import {
  api,
  dayjs,
  message,
  type EventDetail,
  type Occurrence,
  type Tags,
} from "./api";
import { t } from "./i18n";

export function EventPopover({
  event,
  anchor,
  tags,
  zone,
  onClose,
  onEdit,
  onSaved,
}: {
  event: Occurrence;
  anchor: HTMLElement;
  tags: string[];
  zone: string;
  onClose: () => void;
  onEdit: (series: boolean) => void;
  onSaved: () => void;
}) {
  const [detail, setDetail] = useState<EventDetail>();
  const [selected, setSelected] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const { refs, floatingStyles, context } = useFloating({
    open: true,
    onOpenChange: (opened) => {
      if (!opened) onClose();
    },
    elements: { reference: anchor },
    placement: "right-start",
    middleware: [offset(10), flip(), shift({ padding: 12 })],
    whileElementsMounted: autoUpdate,
  });
  const dismiss = useDismiss(context);
  const role = useRole(context);
  const { getFloatingProps } = useInteractions([dismiss, role]);

  useEffect(() => {
    let active = true;
    api<EventDetail>(`/api/events/${event.eventId}`)
      .then((detail) => {
        if (!active) return;
        setDetail(detail);
        const base = detail.tags[""]?.add ?? [];
        const override = event.rid ? detail.tags[event.rid] : undefined;
        setSelected(
          [...new Set([...base, ...(override?.add ?? [])])].filter(
            (tag) => !override?.remove?.includes(tag),
          ),
        );
      })
      .catch((error) => {
        if (active) setError(message(error));
      });
    return () => {
      active = false;
    };
  }, [event.eventId, event.rid]);

  async function toggle(tag: string) {
    if (!detail || saving) return;
    const previous = selected;
    const next = selected.includes(tag)
      ? selected.filter((value) => value !== tag)
      : [...selected, tag];
    const base = detail.tags[""]?.add ?? [];
    const update: Tags = {
      add: event.rid ? next.filter((value) => !base.includes(value)) : next,
      remove: event.rid ? base.filter((value) => !next.includes(value)) : [],
    };
    setSelected(next);
    setSaving(true);
    setError("");
    try {
      const query = event.rid ? `?rid=${encodeURIComponent(event.rid)}` : "";
      await api(`/api/events/${event.eventId}/tags${query}`, "PUT", update);
      onSaved();
    } catch (error) {
      setSelected(previous);
      setError(message(error));
    } finally {
      setSaving(false);
    }
  }

  const choices = [...new Set([...tags, ...selected])].filter(
    (tag) => !tag.startsWith("ics:"),
  );
  const imported = event.tags?.filter((tag) => tag.startsWith("ics:")) ?? [];
  return (
    <FloatingPortal>
      <FloatingFocusManager context={context} modal={false}>
        <Paper
          ref={refs.setFloating}
          style={{
            ...floatingStyles,
            zIndex: 300,
            width: 340,
            maxWidth: "calc(100vw - 24px)",
          }}
          shadow="md"
          withBorder
          p="md"
          radius="md"
          {...getFloatingProps()}
          aria-label={event.title || t("Событие")}
        >
          <Stack gap="sm">
            {event.title && <Text fw={600}>{event.title}</Text>}
            <Text size="sm" c="dimmed">
              {dayjs(event.start)
                .tz(zone)
                .format(event.allDay ? "D MMMM" : "D MMMM, HH:mm")}{" "}
              —{" "}
              {dayjs(event.end)
                .tz(zone)
                .format(event.allDay ? "D MMMM" : "D MMMM, HH:mm")}
            </Text>
            {error && <Alert color="red">{error}</Alert>}
            {!detail && !error && <Loader size="sm" />}
            {detail && (
              <>
                <Group gap="xs" mah={180} style={{ overflowY: "auto" }}>
                  {choices.map((tag) => (
                    <Badge
                      key={tag}
                      component="button"
                      type="button"
                      variant={selected.includes(tag) ? "filled" : "outline"}
                      aria-pressed={selected.includes(tag)}
                      disabled={saving}
                      onClick={() => void toggle(tag)}
                      style={{
                        cursor: saving ? "wait" : "pointer",
                        textTransform: "none",
                      }}
                    >
                      {tag}
                    </Badge>
                  ))}
                  {imported.map((tag) => (
                    <Badge
                      key={tag}
                      color="gray"
                      variant="light"
                      style={{ textTransform: "none" }}
                    >
                      {tag}
                    </Badge>
                  ))}
                </Group>
                {!choices.length && !imported.length && (
                  <Text size="sm" c="dimmed">
                    {t("Добавьте тэги в общих настройках.")}
                  </Text>
                )}
                {event.editable && (
                  <Stack gap="xs">
                    {detail.event.rrule ||
                    detail.event.rdates?.length ||
                    event.rid ? (
                      <>
                        <Button
                          variant="filled"
                          disabled={saving}
                          onClick={() => onEdit(true)}
                        >
                          {t("Редактировать всю серию")}
                        </Button>
                        <Button
                          variant="light"
                          disabled={saving}
                          onClick={() => onEdit(false)}
                        >
                          {t("Редактировать только это событие")}
                        </Button>
                      </>
                    ) : (
                      <Button disabled={saving} onClick={() => onEdit(false)}>
                        {t("Редактировать событие")}
                      </Button>
                    )}
                  </Stack>
                )}
              </>
            )}
          </Stack>
        </Paper>
      </FloatingFocusManager>
    </FloatingPortal>
  );
}
