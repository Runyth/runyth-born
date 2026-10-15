"use strict";

import { createSession, listRecentSessions, saveSession } from "./storage.js";

const elements = {
  grid: document.getElementById("template-grid"),
  loading: document.getElementById("template-loading"),
  error: document.getElementById("template-error"),
  empty: document.getElementById("template-empty"),
  recentSection: document.getElementById("recent-section"),
  recentList: document.getElementById("recent-list"),
  version: document.getElementById("app-version"),
};

function formatShownDate(value) {
  const parsed = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  const pad = (part) => String(part).padStart(2, "0");
  return `${parsed.getFullYear()}/${pad(parsed.getMonth() + 1)}/${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}

function wizardURL(sessionID) {
  const url = new URL("./wizard.html", document.baseURI);
  url.searchParams.set("session", sessionID);
  return url.toString();
}

function templateCard(template) {
  const article = document.createElement("article");
  article.className = "template-card";

  const summary = document.createElement("div");
  const heading = document.createElement("h3");
  heading.textContent = template.name;
  summary.append(heading);

  const details = document.createElement("p");
  const questionWord = template.question_count === 1 ? "question" : "questions";
  const sectionWord = template.section_count === 1 ? "section" : "sections";
  details.textContent = `${template.question_count} ${questionWord} across ${template.section_count} ${sectionWord}`;
  summary.append(details);

  const filename = document.createElement("p");
  filename.className = "filename";
  filename.textContent = template.filename;
  summary.append(filename);
  article.append(summary);

  const form = document.createElement("form");
  form.className = "start-form";

  const label = document.createElement("label");
  const labelText = document.createElement("span");
  labelText.textContent = "Your name ";
  const optional = document.createElement("small");
  optional.textContent = "(optional)";
  labelText.append(optional);
  label.append(labelText);

  const input = document.createElement("input");
  input.type = "text";
  input.maxLength = 100;
  input.autocomplete = "name";
  input.placeholder = "e.g. Alex";
  label.append(input);
  form.append(label);

  const button = document.createElement("button");
  button.className = "button button-primary";
  button.type = "submit";
  button.textContent = "Start this template";
  form.append(button);

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    button.disabled = true;
    button.setAttribute("aria-busy", "true");
    try {
      const session = createSession(template, input.value);
      await saveSession(session, { touch: false });
      window.location.assign(wizardURL(session.id));
    } catch (error) {
      button.disabled = false;
      button.removeAttribute("aria-busy");
      elements.error.textContent = `Could not create the questionnaire locally: ${error.message}`;
      elements.error.hidden = false;
    }
  });

  article.append(form);
  return article;
}

async function renderRecent() {
  if (!elements.recentSection || !elements.recentList) {
    return;
  }
  const sessions = await listRecentSessions();
  if (sessions.length === 0) {
    return;
  }

  elements.recentList.replaceChildren();
  for (const session of sessions) {
    const link = document.createElement("a");
    link.href = wizardURL(session.id);

    const title = document.createElement("strong");
    title.textContent = session.player_name
      ? `${session.template_name} — ${session.player_name}`
      : session.template_name;
    link.append(title);

    const timestamp = document.createElement("small");
    timestamp.textContent = formatShownDate(session.updated_at || session.created_at || Date.now()) || "Resume";
    link.append(timestamp);
    elements.recentList.append(link);
  }
  elements.recentSection.hidden = false;
}

async function loadCatalog() {
  try {
    const response = await fetch(new URL("./templates/catalog.json", document.baseURI), { cache: "no-cache" });
    if (!response.ok) {
      throw new Error(`${response.status} ${response.statusText}`);
    }
    const catalog = await response.json();
    if (!catalog || !Array.isArray(catalog.templates)) {
      throw new Error("catalog format is invalid");
    }

    elements.version.textContent = catalog.app_version || "3.0.2";
    elements.grid.replaceChildren();
    for (const template of catalog.templates) {
      elements.grid.append(templateCard(template));
    }
    elements.empty.hidden = catalog.templates.length !== 0;
  } catch (error) {
    elements.error.textContent = `The static template catalog could not be loaded: ${error.message}`;
    elements.error.hidden = false;
  } finally {
    elements.loading.hidden = true;
    elements.grid.setAttribute("aria-busy", "false");
  }
}

void Promise.all([loadCatalog(), renderRecent()]);
