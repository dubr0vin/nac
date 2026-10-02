import { useTranslation } from "react-i18next";
import "./i18n";
import { createRoot } from "react-dom/client";
import { MantineProvider, createTheme } from "@mantine/core";
import { DatesProvider } from "@mantine/dates";
import "@mantine/core/styles.css";
import "@mantine/dates/styles.css";
import "@mantine/schedule/styles.css";
import "./style.css";
import App from "./App";

const theme = createTheme({
  primaryColor: "teal",
  defaultRadius: "md",
  fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif",
  headings: { fontFamily: "inherit", fontWeight: "650" },
});

function Root() {
  const { i18n } = useTranslation();
  return (
    <MantineProvider theme={theme} defaultColorScheme="auto">
      <DatesProvider settings={{ locale: i18n.language, firstDayOfWeek: 1 }}>
        <App />
      </DatesProvider>
    </MantineProvider>
  );
}

createRoot(document.getElementById("root")!).render(<Root />);
