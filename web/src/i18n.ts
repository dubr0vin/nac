import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import dayjs from "dayjs";
import "dayjs/locale/ru";
import en from "./locales/en.json";
import errors from "./locales/ru.json";

// Russian text is the key, so it needs no duplicate translation file.
const ru = Object.fromEntries(Object.keys(en).map((key) => [key, key]));
void i18n.use(initReactI18next).init({
  resources: {
    ru: { translation: { ...ru, ...errors } },
    en: {
      translation: {
        ...Object.fromEntries(Object.keys(errors).map((key) => [key, key])),
        ...en,
      },
    },
  },
  lng:
    localStorage.getItem("nac.language") ||
    (navigator.language.startsWith("ru") ? "ru" : "en"),
  supportedLngs: ["ru", "en"],
  fallbackLng: "ru",
  keySeparator: false,
  nsSeparator: false,
  interpolation: { escapeValue: false },
});
function applyLanguage(language: string) {
  document.documentElement.lang = language;
  dayjs.locale(language);
  localStorage.setItem("nac.language", language);
}
applyLanguage(i18n.language);
i18n.on("languageChanged", applyLanguage);

export const t = i18n.t.bind(i18n);
export default i18n;

export function scheduleLabels() {
  return {
    day: t("День"),
    week: t("Неделя"),
    month: t("Месяц"),
    year: t("Год"),
    allDay: t("Весь день").replaceAll(" ", "\u00a0"),
    today: t("Сегодня"),
    previous: t("Назад"),
    next: t("Вперёд"),
    noEvents: t("Нет событий"),
    agenda: t("Список"),
    resource: t("Участник"),
    resources: t("Участники"),
    resourceSlot: t("Интервал участника"),
    weekday: t("День недели"),
    timeSlot: t("Интервал времени"),
    more: t("Ещё"),
    selectMonth: t("Выбрать месяц"),
    selectYear: t("Выбрать год"),
    switchToDayView: t("Показать день"),
    switchToWeekView: t("Показать неделю"),
    switchToMonthView: t("Показать месяц"),
    switchToYearView: t("Показать год"),
    viewSelectLabel: t("Вид календаря"),
    moreLabel: (count: number) => t("Ещё {{count}}", { count }),
  };
}
