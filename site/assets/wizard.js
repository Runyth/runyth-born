"use strict";

import { buildResumeURL, decodeResumePayload, resumePayloadFromLocation } from "./portable.js";
import { callCore, preservedAnswersMarkdown } from "./runtime.js";
import {
  answeredEntries,
  dedupeEntries,
  deleteRecovery,
  deleteSession,
  loadRecovery,
  loadSession,
  normalizeRevision,
  recordsDiverge,
  saveRecovery,
  saveSession,
  saveSessionPosition,
  SessionDeletedError,
  StorageConflictError,
} from "./storage.js";

const elements = {
  app: document.getElementById("wizard-app"),
  card: document.getElementById("question-card"),
  fatal: document.getElementById("fatal-error"),
  sectionList: document.getElementById("section-list"),
  questionNumber: document.getElementById("question-number"),
  questionSection: document.getElementById("question-section"),
  context: document.getElementById("question-context"),
  label: document.getElementById("question-label"),
  answer: document.getElementById("answer"),
  markAnswered: document.getElementById("mark-answered"),
  previous: document.getElementById("previous-question"),
  next: document.getElementById("next-question"),
  saveStatus: document.getElementById("save-status"),
  overallCount: document.getElementById("overall-count"),
  overallProgress: document.getElementById("overall-progress"),
  filters: {
    new: document.getElementById("filter-new"),
    skipped: document.getElementById("filter-skipped"),
    marked: document.getElementById("filter-marked"),
  },
  filterClear: document.getElementById("filter-clear"),
  filterEmpty: document.getElementById("filter-empty"),
  download: document.getElementById("download-markdown"),
  finish: document.getElementById("finish-download"),
  copyLink: document.getElementById("copy-link"),
  helpDialog: document.getElementById("help-dialog"),
  openHelp: document.getElementById("open-help"),
  toc: document.getElementById("toc"),
  openToc: document.getElementById("open-toc"),
  closeToc: document.getElementById("close-toc"),
  toast: document.getElementById("toast"),
  finishToast: document.getElementById("finish-toast"),
  finishDialog: document.getElementById("finish-dialog"),
  finishResumeLink: document.getElementById("finish-resume-link"),
  finishCopyLink: document.getElementById("finish-copy-link"),
  finishDelete: document.getElementById("finish-delete"),
  reconcileNotice: document.getElementById("reconcile-notice"),
  reconcileText: document.getElementById("reconcile-text"),
  reviewPreserved: document.getElementById("review-preserved"),
  orphanDialog: document.getElementById("orphan-dialog"),
  orphanList: document.getElementById("orphan-list"),
  orphanCopy: document.getElementById("orphan-copy"),
  orphanDiscard: document.getElementById("orphan-discard"),
  orphanImport: document.getElementById("orphan-import"),
  title: document.getElementById("wizard-template-name"),
  player: document.getElementById("wizard-player-name"),
};

let session = null;
let catalogEntry = null;
let templateSource = "";
let sections = [];
let currentIndex = 0;
let filters = new Set();
let saveTimer = null;
let savePromise = Promise.resolve();
let dirty = false;
let editRevision = 0;
let persistedRevision = 0;
let toastTimer = null;
let navigating = false;
let ready = false;
let preservedEntries = [];
let preservedReason = "reconcile";
let deleteArmed = false;

const CONFLICT_STATUS = "A newer version was saved elsewhere — reload to refresh";

function enableActions(enabled) {
  ready = enabled;
  for (const element of [elements.previous, elements.next, elements.download, elements.finish,
    elements.copyLink, elements.finishCopyLink]) {
    if (element) { element.disabled = !enabled; }
  }
}
enableActions(false);

function setSaveStatus(message, kind = "") {
  elements.saveStatus.textContent = message;
  elements.saveStatus.className = `save-status ${kind}`.trim();
}

function showToast(message, kind = "") {
  clearTimeout(toastTimer);
  elements.toast.hidden = true;
  elements.toast.className = "toast";
  if (elements.finishToast) {
    elements.finishToast.hidden = true;
    elements.finishToast.className = "toast";
  }

  const target = kind === "copy-success" && elements.finishToast ? elements.finishToast : elements.toast;
  target.textContent = message;
  target.className = `toast${kind ? ` ${kind}` : ""}`;
  target.hidden = false;
  toastTimer = setTimeout(() => {
    target.hidden = true;
    target.className = "toast";
  }, 2400);
}

function normalizeAnswer(value) {
  return String(value).replaceAll("\r\n", "\n").replaceAll("\r", "\n").trim();
}

function question() {
  return session?.questions?.[currentIndex] || null;
}

function currentSection() {
  return sections.find((section) => currentIndex >= section.start && currentIndex <= section.end);
}

function sectionTitle(section) {
  if (!section) {
    return "";
  }
  const escapedIndex = String(section.index).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const withoutDuplicateNumber = section.title.replace(
    new RegExp(`^\\s*${escapedIndex}\\s*[.)]\\s*`),
    "",
  ).trim();
  return withoutDuplicateNumber || section.title;
}

function isFilled(item) {
  return String(item?.answer || "").trim() !== "";
}

function isMarkedBlank(item) {
  return item?.marked_answered === true && !isFilled(item);
}

function filtersActive() {
  return filters.size > 0;
}

function isNewBlank(item) {
  return Boolean(item) && !isFilled(item) && !isMarkedBlank(item) && item.skipped !== true;
}

function isSkippedBlank(item) {
  return Boolean(item) && !isFilled(item) && !isMarkedBlank(item) && item.skipped === true;
}

function questionMatches(item) {
  if (!filtersActive()) {
    return true;
  }
  if (filters.has("new") && isNewBlank(item)) { return true; }
  if (filters.has("skipped") && isSkippedBlank(item)) { return true; }
  if (filters.has("marked") && isMarkedBlank(item)) { return true; }
  return false;
}

function neighborIndex(direction) {
  if (!session) { return null; }
  const stepBy = direction < 0 ? -1 : 1;
  for (let index = currentIndex + stepBy; index >= 0 && index < session.questions.length; index += stepBy) {
    if (questionMatches(session.questions[index])) { return index; }
  }
  return null;
}

function firstMatchInSection(section) {
  for (let index = section.start; index <= section.end; index += 1) {
    if (questionMatches(session.questions[index])) { return index; }
  }
  return null;
}

function noteSkip() {
  const item = question();
  if (!item || isFilled({ answer: elements.answer.value }) || markPressed() || item.skipped === true) {
    return;
  }
  item.skipped = true;
  dirty = true;
}

function renderFilterButtons() {
  const counts = { new: 0, skipped: 0, marked: 0 };
  for (const item of session?.questions || []) {
    if (isNewBlank(item)) { counts.new += 1; }
    if (isSkippedBlank(item)) { counts.skipped += 1; }
    if (isMarkedBlank(item)) { counts.marked += 1; }
  }
  for (const name of ["new", "skipped", "marked"]) {
    const button = elements.filters[name];
    button.setAttribute("aria-pressed", filters.has(name) ? "true" : "false");
    button.querySelector(".filter-count").textContent = String(counts[name]);
  }
  elements.filterClear.disabled = filters.size === 0;
}

function markPressed() {
  return elements.markAnswered.getAttribute("aria-pressed") === "true";
}

function syncMarkButton(pressed, locked = false) {
  const on = pressed && !locked;
  elements.markAnswered.disabled = locked;
  elements.markAnswered.setAttribute("aria-pressed", on ? "true" : "false");
  elements.markAnswered.className = on ? "button button-marked" : "button button-secondary";
  if (locked) {
    elements.markAnswered.title = "Clear the answer before marking this question blank";
  } else {
    elements.markAnswered.removeAttribute("title");
  }
  elements.markAnswered.replaceChildren();
  if (on) {
    const check = document.createElement("span");
    check.className = "mark-check";
    check.setAttribute("aria-hidden", "true");
    check.textContent = "✓";
    elements.markAnswered.append(check);
  }
  elements.markAnswered.append("Mark answered");
}

function sectionProgress(section) {
  const items = session.questions.slice(section.start, section.end + 1);
  return {
    filled: items.filter(isFilled).length,
    marked: items.filter(isMarkedBlank).length,
    total: items.length,
  };
}

function sectionTotal(section) {
  return Math.max(0, section.end - section.start + 1);
}

function renderContext(items) {
  elements.context.replaceChildren();
  for (const item of items || []) {
    const paragraph = document.createElement("p");
    paragraph.textContent = item;
    elements.context.append(paragraph);
  }
}

function renderSections() {
  elements.sectionList.replaceChildren();
  const active = currentSection();
  for (const section of sections) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `section-button${active && active.index === section.index ? " active" : ""}`;

    const title = document.createElement("strong");
    title.textContent = `${section.index}. ${sectionTitle(section)}`;
    button.append(title);

    const progress = sectionProgress(section);
    const count = document.createElement("span");
    count.className = "section-count";
    const filled = document.createElement("span");
    filled.className = "count-filled";
    filled.textContent = String(progress.filled);
    count.append(filled);
    if (progress.marked > 0) {
      const marked = document.createElement("span");
      marked.className = "count-marked";
      marked.textContent = `+${progress.marked}`;
      count.append(marked);
    }
    count.append(`/${progress.total}`);
    button.append(count);
    if (progress.marked > 0) {
      button.classList.add("has-marked");
    }

    const target = firstMatchInSection(section);
    if (filtersActive() && target === null) {
      button.disabled = true;
      button.title = "No matching questions in this section";
    }
    button.addEventListener("click", () => {
      if (target === null) {
        showToast("No matching questions in that section.");
        return;
      }
      void navigateTo(target);
      elements.toc.classList.remove("open");
    });
    elements.sectionList.append(button);
  }
}

function renderProgress() {
  const filled = session.questions.filter(isFilled).length;
  const marked = session.questions.filter(isMarkedBlank).length;
  const total = session.questions.length;
  const done = filled + marked;
  elements.overallCount.replaceChildren();
  const written = document.createElement("span");
  written.className = "count-filled";
  written.textContent = String(filled);
  elements.overallCount.append(written, " written");
  if (marked > 0) {
    const blank = document.createElement("span");
    blank.className = "count-marked";
    blank.textContent = `${marked} marked blank`;
    elements.overallCount.append(" · ", blank);
  }
  elements.overallCount.append(` · ${done} of ${total}`);
  const filledWidth = total === 0 ? 0 : (filled / total) * 100;
  const markedWidth = total === 0 ? 0 : (marked / total) * 100;
  elements.overallProgress.querySelector(".progress-filled").style.width = `${filledWidth}%`;
  elements.overallProgress.querySelector(".progress-marked").style.width = `${markedWidth}%`;
  const summary = marked > 0
    ? `${filled} written, ${marked} marked blank, ${done} of ${total}`
    : `${filled} of ${total} answered`;
  elements.overallProgress.setAttribute("aria-valuenow", String(total === 0 ? 0 : Math.round(((filled + marked) / total) * 100)));
  elements.overallProgress.setAttribute("aria-valuetext", summary);
  renderFilterButtons();
}

function renderQuestion({ focus = false } = {}) {
  const item = question();
  const section = currentSection();
  if (!item) {
    fatal("The selected question no longer exists.");
    return;
  }

  if (filtersActive() && !questionMatches(item)) {
    elements.card.hidden = true;
    elements.filterEmpty.hidden = false;
    elements.previous.disabled = true;
    elements.next.disabled = true;
    renderSections();
    renderProgress();
    elements.app.setAttribute("aria-busy", "false");
    return;
  }
  elements.filterEmpty.hidden = true;

  const shown = filtersActive()
    ? session.questions.reduce((count, entry, index) => count + (questionMatches(entry) && index <= currentIndex ? 1 : 0), 0)
    : 0;
  const shownTotal = filtersActive()
    ? session.questions.filter((entry) => questionMatches(entry)).length
    : 0;
  elements.questionNumber.textContent = filtersActive()
    ? `Question ${currentIndex + 1} of ${session.questions.length} · ${shown} of ${shownTotal} shown`
    : `Question ${currentIndex + 1} of ${session.questions.length}`;
  if (section) {
    const questionInSection = currentIndex - section.start + 1;
    elements.questionSection.textContent = `Section ${section.index} · Question ${questionInSection} of ${sectionTotal(section)} — ${sectionTitle(section)}`;
  } else {
    elements.questionSection.textContent = "";
  }

  renderContext(item.context);
  elements.label.textContent = item.label;
  elements.answer.value = item.answer || "";
  syncMarkButton(isMarkedBlank(item), isFilled(item));
  elements.card.classList.toggle("marked-blank", isMarkedBlank(item));
  const onLastQuestion = currentIndex === session.questions.length - 1;
  elements.previous.disabled = false;
  elements.next.disabled = false;
  const atFilterStart = filtersActive() && neighborIndex(-1) === null;
  elements.previous.setAttribute("aria-label", currentIndex === 0 || atFilterStart
    ? (filtersActive() ? "Already at the first matching question" : "Already at the first question")
    : "Previous question");
  elements.next.className = onLastQuestion ? "button button-success" : "button button-primary";
  elements.next.textContent = onLastQuestion ? elements.finish.textContent : "Next →";
  elements.next.setAttribute("aria-label", onLastQuestion ? "Finish and download questionnaire" : "Next question");
  renderSections();
  renderProgress();
  elements.card.hidden = false;
  elements.app.setAttribute("aria-busy", "false");
  dirty = false;
  if (focus) {
    elements.answer.focus();
  }
}

function updateLocalAnswer() {
  if (!session || !question()) {
    return;
  }
  const filled = isFilled({ answer: elements.answer.value });
  syncMarkButton(filled ? false : markPressed(), filled);
  session.questions[currentIndex].answer = elements.answer.value;
  session.questions[currentIndex].marked_answered = markPressed();
  if (isFilled(session.questions[currentIndex]) || markPressed()) {
    session.questions[currentIndex].skipped = false;
  }
  elements.card.classList.toggle("marked-blank", isMarkedBlank(session.questions[currentIndex]));
  if (session.completed && dirty) {
    session.completed = false;
    elements.finish.textContent = "Finish and download";
    if (currentIndex === session.questions.length - 1) {
      elements.next.textContent = elements.finish.textContent;
    }
  }
  renderProgress();
  renderSections();
}

function scheduleSave() {
  dirty = true;
  updateLocalAnswer();
  editRevision += 1;
  clearTimeout(saveTimer);
  setSaveStatus("Unsaved changes", "saving");
  saveTimer = setTimeout(() => {
    void flushSave().catch(() => undefined);
  }, 400);
}

function flushSave() {
  clearTimeout(saveTimer);
  saveTimer = null;
  if (!session || !dirty) {
    return savePromise;
  }

  const index = currentIndex;
  const revision = editRevision;
  session.questions[index].answer = normalizeAnswer(elements.answer.value);
  if (isFilled(session.questions[index])) {
    syncMarkButton(false, true);
  }
  session.questions[index].marked_answered = markPressed();
  const snapshot = structuredClone(session);
  setSaveStatus("Saving locally…", "saving");
  savePromise = savePromise
    .catch(() => undefined)
    .then(async () => {
      // Carry the currently acknowledged revision into the queued snapshot at
      // execution time, so this tab's serialized saves never conflict with
      // their own predecessor (F09), while external stale copies still
      // conflict through the stored revision.
      snapshot.revision = normalizeRevision(session.revision);
      const saved = await saveSession(snapshot);
      session.updated_at = saved.updated_at;
      session.revision = saved.revision;
      persistedRevision = Math.max(persistedRevision, revision);
      dirty = editRevision > persistedRevision;
      setSaveStatus(
        dirty ? "Unsaved changes" : (session.completed ? "Finished and saved locally" : "Saved locally"),
        dirty ? "saving" : "saved",
      );
    })
    .catch(async (error) => {
      if (error instanceof SessionDeletedError) {
        // Deletion invalidates earlier writer generations (F11): the queued
        // edit is not acknowledged and never recreates the session.
        dirty = false;
        setSaveStatus("Removed locally in another tab", "error");
        showToast("This questionnaire was removed from this device; the current edits stay in this tab only.");
        return;
      }
      if (error instanceof StorageConflictError) {
        try {
          const recovery = await preserveRecovery(session.id, "conflict", session.questions);
          preservedEntries = recovery?.entries || preservedEntries;
          showPreservedNotice(preservedEntries.length, "conflict");
        } catch {
          showToast("Could not preserve the rejected edit locally");
        }
        setSaveStatus(CONFLICT_STATUS, "conflict");
        return;
      }
      setSaveStatus("Local save failed", "error");
      showToast(`Could not save locally: ${error.message}`);
      throw error;
    });
  return savePromise;
}

async function persistPosition() {
  session.current_question = currentIndex;
  await saveSessionPosition(session.id, currentIndex);
}

async function navigateTo(index) {
  if (!ready || navigating) {
    return;
  }
  navigating = true;
  elements.answer.disabled = true;
  try {
    updateLocalAnswer();
    try {
      await flushSave();
    } catch {
      return;
    }

    if (index < 0) {
      showToast("That was the first question.");
      return;
    }
    if (index >= session.questions.length) {
      showToast("That was the last question.");
      return;
    }

    currentIndex = index;
    try {
      await persistPosition();
    } catch (error) {
      setSaveStatus("Position not saved", "error");
      showToast(`Could not save position: ${error.message}`);
    }
    renderQuestion({ focus: true });
    document.querySelector(".question-area")?.scrollTo({ top: 0, behavior: "smooth" });
  } finally {
    navigating = false;
    elements.answer.disabled = false;
    elements.answer.focus();
  }
}

function fatal(message) {
  enableActions(false);
  elements.fatal.textContent = message;
  elements.fatal.hidden = false;
  elements.card.hidden = true;
  elements.app.setAttribute("aria-busy", "false");
  setSaveStatus("Unavailable", "error");
}

function asciiSlug(value) {
  return String(value)
    .toLowerCase()
    .replace(/[^a-z0-9\s_-]/gu, "")
    .trim()
    .replace(/[\s_]+/gu, "-")
    .replace(/-+/gu, "-") || "filled-template";
}

function downloadBlob(content, filename) {
  const blob = new Blob([content], { type: "text/markdown;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.hidden = true;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

async function prepareMarkdown() {
  const result = await callCore("reconcile", {
    source: templateSource,
    extension: catalogEntry.extension,
    saved_questions: session.questions,
  });
  session.questions = result.questions;
  sections = result.sections;
  session.template_fingerprint = result.fingerprint;
  return result.markdown;
}

async function portableResumeLink() {
  updateLocalAnswer();
  await flushSave();
  return buildResumeURL(session);
}

async function showFinishDialog() {
  if (!elements.finishDialog) {
    return;
  }
  deleteArmed = false;
  if (elements.finishDelete) {
    elements.finishDelete.textContent = "Delete local copy";
  }
  elements.finishResumeLink.value = "Preparing portable resume link…";
  elements.finishDialog.showModal();
  try {
    const url = await portableResumeLink();
    elements.finishResumeLink.value = url;
  } catch (error) {
    elements.finishResumeLink.value = "";
    showToast(`Could not create resume link: ${error.message}`);
  }
}

async function download(finish) {
  if (!ready) { return; }
  updateLocalAnswer();
  try {
    await flushSave();
    if (finish) {
      elements.finish.disabled = true;
      elements.next.disabled = true;
      session.completed = true;
      const saved = await saveSession(session);
      session.revision = saved.revision;
      session.updated_at = saved.updated_at;
      elements.finish.textContent = "Finished";
      if (currentIndex === session.questions.length - 1) {
        elements.next.textContent = "Finished";
      }
    }

    const markdown = await prepareMarkdown();
    const filename = `${asciiSlug([session.template_name, session.player_name].filter(Boolean).join("-"))}.md`;
    downloadBlob(markdown, filename);
    if (finish) {
      await showFinishDialog();
    }
  } catch (error) {
    showToast(`Could not prepare the file: ${error.message}`);
  } finally {
    elements.finish.disabled = false;
    elements.next.disabled = false;
  }
}

async function deleteLocalCopy() {
  if (!session) { return; }
  if (!deleteArmed) {
    deleteArmed = true;
    if (elements.finishDelete) {
      elements.finishDelete.textContent = "Confirm delete";
    }
    return;
  }
  try {
    await deleteSession(session.id);
    elements.finishDialog?.close();
    window.location.assign("./index.html");
  } catch (error) {
    deleteArmed = false;
    if (elements.finishDelete) {
      elements.finishDelete.textContent = "Delete local copy";
    }
    showToast(`Could not delete the local copy: ${error.message}`);
  }
}

async function copyResumeLink({ prominent = false } = {}) {
  try {
    const value = await portableResumeLink();
    if (elements.finishResumeLink) {
      elements.finishResumeLink.value = value;
    }
    try {
      await navigator.clipboard.writeText(value);
      showToast("✓ Portable resume link copied.", prominent ? "copy-success" : "");
      return;
    } catch {
      // Continue with a selection fallback below.
    }

    if (prominent && elements.finishResumeLink) {
      elements.finishResumeLink.focus();
      elements.finishResumeLink.select();
      try {
        if (document.execCommand("copy")) {
          showToast("✓ Portable resume link copied.", "copy-success");
          return;
        }
      } catch {
        // Continue to the explicit manual-copy prompt.
      }
    }
    window.prompt("Copy this private portable resume link:", value);
  } catch (error) {
    showToast(`Could not create resume link: ${error.message}`);
  }
}

async function loadCatalog() {
  const response = await fetch(new URL("./templates/catalog.json", document.baseURI), { cache: "no-cache" });
  if (!response.ok) {
    throw new Error(`template catalog returned ${response.status}`);
  }
  const catalog = await response.json();
  if (!catalog || !Array.isArray(catalog.templates)) {
    throw new Error("template catalog format is invalid");
  }
  return catalog;
}

async function importPortableSession() {
  const payload = resumePayloadFromLocation();
  if (!payload) {
    return null;
  }

  const imported = await decodeResumePayload(payload);
  const existing = await loadSession(imported.id);
  if (!existing || existing.template_filename !== imported.template_filename) {
    return { session: imported, storedAlready: false };
  }

  // Revisions, not wall clocks, decide which divergent copy is newer (F03).
  // Every divergent branch is preserved regardless of the local counter, so a
  // lower-revision portable copy is never silently dropped (F10).
  const existingRevision = normalizeRevision(existing.revision);
  const importedRevision = normalizeRevision(imported.revision);
  if (recordsDiverge(existing, imported)) {
    if (importedRevision > existingRevision) {
      await preserveRecovery(imported.id, "import", existing.questions);
      return { session: imported, storedAlready: false };
    }
    await preserveRecovery(imported.id, "import", imported.questions);
    return { session: existing, storedAlready: true };
  }
  return { session: existing, storedAlready: true };
}

async function preserveRecovery(id, reason, questions, fingerprint) {
  const existing = await loadRecovery(id).catch(() => null);
  const entries = dedupeEntries([...(existing?.entries || []), ...answeredEntries(questions)]);
  if (entries.length === 0) {
    return null;
  }
  return saveRecovery(id, {
    reason,
    fingerprint: fingerprint ?? existing?.fingerprint ?? null,
    entries,
  });
}

const preservedExplanations = {
  reconcile: (count) => count === 1
    ? "1 saved answer no longer fits this questionnaire and was preserved for review."
    : `${count} saved answers no longer fit this questionnaire and were preserved for review.`,
  import: (count) => count === 1
    ? "1 answer from the opened resume link diverged from the saved questionnaire and was preserved for review."
    : `${count} answers from the opened resume link diverged from the saved questionnaire and were preserved for review.`,
  conflict: (count) => count === 1
    ? "1 edited answer could not be saved because a newer version exists elsewhere and was preserved for review."
    : `${count} edited answers could not be saved because a newer version exists elsewhere and were preserved for review.`,
  divergent: (count) => count === 1
    ? "1 answer from a divergent copy of this questionnaire was preserved for review."
    : `${count} answers from a divergent copy of this questionnaire were preserved for review.`,
};

function showPreservedNotice(count, reason) {
  preservedReason = reason || preservedReason;
  elements.reconcileText.textContent =
    (preservedExplanations[preservedReason] || preservedExplanations.reconcile)(count);
  elements.reconcileNotice.hidden = false;
}

function refreshPreservedEntries(recovery) {
  preservedEntries = dedupeEntries([...(recovery?.entries || []), ...preservedEntries]);
  if (preservedEntries.length > 0) {
    showPreservedNotice(preservedEntries.length, recovery?.reason);
  }
}

function showOrphanDialog() {
  elements.orphanList.replaceChildren();
  for (const entry of preservedEntries) {
    const item = document.createElement("div");
    item.className = "orphan-entry";

    const heading = document.createElement("strong");
    heading.textContent = entry.label;
    item.append(heading);

    if (entry.breadcrumb.length > 0) {
      const path = document.createElement("small");
      path.textContent = entry.breadcrumb.join(" › ");
      item.append(path);
    }

    const answer = document.createElement("p");
    answer.textContent = entry.answer;
    item.append(answer);

    elements.orphanList.append(item);
  }
  // Choosing the imported branch is only meaningful when the preserved copy
  // came from an import; reconciled and rejected edits stay review-only.
  elements.orphanImport.hidden = preservedReason !== "import";
  elements.orphanDialog.showModal();
}

async function copyPreservedEntries() {
  const content = preservedAnswersMarkdown(preservedEntries);
  try {
    await navigator.clipboard.writeText(content);
    showToast("✓ Preserved answers copied as Markdown.", "copy-success");
  } catch {
    window.prompt("Copy the preserved answers:", content);
  }
}

async function discardPreservedEntries() {
  await deleteRecovery(session.id);
  preservedEntries = [];
  elements.reconcileNotice.hidden = true;
  showToast("Preserved copy discarded.");
}

// Offers the imported branch as the questionnaire's answers: the preserved
// entries are reconciled through the normal match rules instead of being
// silently preferred or ignored (F10).
async function importPreservedEntries() {
  if (!session || preservedEntries.length === 0) {
    return;
  }
  const result = await callCore("reconcile", {
    source: templateSource,
    extension: catalogEntry.extension,
    saved_questions: preservedEntries,
  });
  const leftovers = (result.reconciliation?.orphaned || [])
    .filter((item) => String(item.answer || "").trim() !== "");
  session.questions = result.questions;
  sections = result.sections;
  session.template_fingerprint = result.fingerprint;
  const saved = await saveSession(session, { touch: true });
  session.revision = saved.revision;
  session.updated_at = saved.updated_at;
  currentIndex = Math.min(Math.max(0, Math.floor(Number(session.current_question) || 0)), session.questions.length - 1);
  session.current_question = currentIndex;
  await deleteRecovery(session.id);
  preservedEntries = [];
  elements.reconcileNotice.hidden = true;
  elements.orphanDialog.close();
  renderQuestion({ focus: true });
  setSaveStatus("Saved locally", "saved");
  if (leftovers.length > 0) {
    const recovery = await preserveRecovery(session.id, "import", leftovers);
    preservedEntries = recovery?.entries || [];
    showPreservedNotice(preservedEntries.length, "import");
  }
}

function firstUnanswered(questions) {
  const index = questions.findIndex((item) => !isFilled(item) && !isMarkedBlank(item));
  return index < 0 ? 0 : index;
}

async function initialise() {
  try {
    let storedAlready = false;
    const imported = await importPortableSession();
    if (imported) {
      session = imported.session;
      storedAlready = imported.storedAlready;
    } else {
      const sessionID = new URL(window.location.href).searchParams.get("session");
      session = await loadSession(sessionID);
      storedAlready = Boolean(session);
    }
    if (!session) {
      throw new Error("This questionnaire is not stored in this browser. Start a new one or open a portable resume link.");
    }

    const catalog = await loadCatalog();
    catalogEntry = catalog.templates.find((entry) => entry.id === session.template_id)
      || catalog.templates.find((entry) => entry.filename === session.template_filename);
    if (!catalogEntry) {
      throw new Error(`Template ${session.template_filename} is no longer present in this site.`);
    }

    const response = await fetch(new URL(catalogEntry.url, document.baseURI), { cache: "no-cache" });
    if (!response.ok) {
      throw new Error(`Template file returned ${response.status}`);
    }
    templateSource = await response.text();

    const freshSession = !Array.isArray(session.questions) || session.questions.length === 0;
    const priorFingerprint = session.template_fingerprint;
    const previousQuestionID = session.questions?.[session.current_question]?.id;
    const savedQuestions = freshSession ? [] : structuredClone(session.questions);
    const result = await callCore("reconcile", {
      source: templateSource,
      extension: catalogEntry.extension,
      saved_questions: savedQuestions,
    });

    const unmatched = (result.reconciliation?.orphaned || [])
      .filter((item) => String(item.answer || "").trim() !== "");
    session.questions = result.questions;
    sections = result.sections;
    session.template_name = result.template_name || catalogEntry.name;
    session.template_filename = catalogEntry.filename;
    session.template_id = catalogEntry.id;
    session.template_fingerprint = result.fingerprint;
    if (freshSession) {
      session.current_question = firstUnanswered(session.questions);
    } else if (priorFingerprint !== result.fingerprint && previousQuestionID) {
      const relocated = session.questions.findIndex((item) => item.id === previousQuestionID);
      if (relocated >= 0) { session.current_question = relocated; }
    }
    currentIndex = Math.min(Math.max(0, Math.floor(Number(session.current_question) || 0)), session.questions.length - 1);
    session.current_question = currentIndex;

    if (unmatched.length > 0) {
      const recovery = await preserveRecovery(session.id, "reconcile", savedQuestions, priorFingerprint);
      preservedEntries = recovery?.entries || [];
      showPreservedNotice(preservedEntries.length, recovery?.reason || "reconcile");
    } else {
      const recovery = await loadRecovery(session.id).catch(() => null);
      preservedEntries = recovery?.entries || [];
      if (preservedEntries.length > 0) {
        showPreservedNotice(preservedEntries.length, recovery?.reason);
      }
    }

    // Persist the session only when its content actually changed, so reloading
    // does not advance the save revision or rewrite acknowledged answers.
    if (!storedAlready || freshSession || priorFingerprint !== result.fingerprint) {
      try {
        const saved = await saveSession(session, { touch: priorFingerprint !== result.fingerprint });
        session.revision = saved.revision;
        session.updated_at = saved.updated_at;
      } catch (error) {
        if (error instanceof StorageConflictError) {
          setSaveStatus(CONFLICT_STATUS, "conflict");
        } else {
          throw error;
        }
      }
    }
    if (resumePayloadFromLocation()) {
      const cleanURL = new URL("./wizard.html", window.location.href);
      cleanURL.searchParams.set("session", session.id);
      cleanURL.hash = "";
      history.replaceState({}, "", cleanURL);
    }
    document.title = `${session.template_name} · Runyth Born`;
    elements.title.textContent = session.template_name;
    elements.player.textContent = session.player_name || "";
    elements.player.hidden = !session.player_name;
    if (session.completed) {
      elements.finish.textContent = "Finished";
    }
    enableActions(true);
    renderQuestion();
    setSaveStatus(session.completed ? "Finished and saved locally" : "Saved locally", "saved");
  } catch (error) {
    fatal(`Could not load this questionnaire: ${error.message}`);
  }
}

elements.answer.addEventListener("input", scheduleSave);
elements.markAnswered.addEventListener("click", () => {
  if (elements.markAnswered.disabled || isFilled({ answer: elements.answer.value })) {
    syncMarkButton(false, true);
    return;
  }
  syncMarkButton(!markPressed());
  scheduleSave();
});
elements.answer.addEventListener("blur", () => {
  void flushSave().catch(() => undefined);
});
async function step(direction) {
  if (!ready || navigating) { return; }
  if (direction > 0 && !elements.card.hidden) { noteSkip(); }
  const target = neighborIndex(direction);
  if (target === null) {
    if (direction > 0 && dirty) {
      updateLocalAnswer();
      try { await flushSave(); } catch { /* status already shown */ }
    }
    if (filtersActive() && !questionMatches(question())) {
      const fallback = neighborIndex(-1);
      if (fallback !== null) {
        showToast("That was the last matching question.");
        await navigateTo(fallback);
        return;
      }
      renderQuestion();
    }
    renderFilterButtons();
    showToast(filtersActive()
      ? (direction < 0 ? "That was the first matching question." : "That was the last matching question.")
      : (direction < 0 ? "That was the first question." : "That was the last question."));
    return;
  }
  await navigateTo(target);
}

async function settleFilter() {
  renderFilterButtons();
  if (!session || !ready) { return; }
  if (questionMatches(question())) {
    renderQuestion();
    return;
  }
  const target = neighborIndex(1) ?? neighborIndex(-1);
  if (target === null) {
    renderQuestion();
    return;
  }
  await navigateTo(target);
}

function toggleFilter(name) {
  if (filters.has(name)) { filters.delete(name); } else { filters.add(name); }
  void settleFilter();
}

elements.filters.new.addEventListener("click", () => toggleFilter("new"));
elements.filters.skipped.addEventListener("click", () => toggleFilter("skipped"));
elements.filters.marked.addEventListener("click", () => toggleFilter("marked"));
elements.filterClear.addEventListener("click", () => {
  if (filters.size === 0) { return; }
  filters.clear();
  void settleFilter();
});
elements.previous.addEventListener("click", () => void step(-1));
elements.next.addEventListener("click", () => {
  if (!ready) { return; }
  if (currentIndex === session.questions.length - 1) {
    void download(true);
    return;
  }
  void step(1);
});
elements.download.addEventListener("click", () => void download(false));
elements.finish.addEventListener("click", () => void download(true));
elements.copyLink.addEventListener("click", () => void copyResumeLink());
elements.finishCopyLink?.addEventListener("click", () => void copyResumeLink({ prominent: true }));
elements.finishDelete?.addEventListener("click", () => void deleteLocalCopy());
elements.reviewPreserved?.addEventListener("click", showOrphanDialog);
elements.orphanCopy?.addEventListener("click", () => void copyPreservedEntries());
elements.orphanImport?.addEventListener("click", () => void importPreservedEntries().catch(async (error) => {
  if (error instanceof StorageConflictError) {
    setSaveStatus(CONFLICT_STATUS, "conflict");
    showToast("A newer version was saved elsewhere — reload to refresh");
    return;
  }
  showToast(`Could not import the preserved answers: ${error.message}`);
}));
elements.orphanDiscard?.addEventListener("click", () => void discardPreservedEntries().catch((error) => {
  showToast(`Could not discard the preserved copy: ${error.message}`);
}));
elements.openHelp.addEventListener("click", () => elements.helpDialog.showModal());
elements.openToc?.addEventListener("click", () => elements.toc.classList.add("open"));
elements.closeToc?.addEventListener("click", () => elements.toc.classList.remove("open"));

document.addEventListener("keydown", (event) => {
  if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
    return;
  }
  if (event.key === "ArrowLeft") {
    event.preventDefault();
    void step(-1);
  } else if (event.key === "ArrowRight") {
    event.preventDefault();
    void step(1);
  } else if (event.key === "Home") {
    event.preventDefault();
    if (event.repeat || !ready || elements.markAnswered.disabled) {
      return;
    }
    elements.markAnswered.click();
  }
});

window.addEventListener("beforeunload", (event) => {
  if (dirty) {
    event.preventDefault();
  }
});

void initialise();
