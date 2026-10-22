"use strict";

// Templates are parsed and laid out at build time. The client needs only JS/JSON.
function fallbackLabel(question) {
  return String(question.fallback_id || "").replace(/#\d+$/u, "");
}

function sectionsFor(questions) {
  const sections = [];
  for (const [index, question] of questions.entries()) {
    const title = question.breadcrumb?.[1] || question.breadcrumb?.[0] || "Questions";
    if (sections.at(-1)?.title !== title) {
      sections.push({ index: sections.length + 1, title, start: index, end: index, total: 0, answered: 0 });
    }
    const section = sections.at(-1);
    section.end = index;
    section.total += 1;
    section.answered += question.answer.trim() ? 1 : 0;
  }
  return sections;
}

export function escapeAnswer(value) {
  // Answers are literal text, including punctuation starting continuation
  // lines and HTML-entity strings such as &copy;.
  return String(value).replace(/[\\`*_{}\[\]()<>!#~+\-=.|&]/gu, "\\$&");
}

// Renders a preserved-answer record with the same literal-text contract as
// ordinary exported Markdown: headings and answers are escaped, entity
// strings, heading markers, and raw HTML cannot change meaning (F07).
export function preservedAnswersMarkdown(entries) {
  let output = "# Preserved answers\n\n";
  for (const entry of entries || []) {
    const heading = [...(entry.breadcrumb || []), entry.label].filter(Boolean).join(" › ") || "Unmatched answer";
    output += `## ${escapeAnswer(heading)}\n\n`;
    const answer = String(entry.answer || "").replace(/\r\n?/gu, "\n");
    if (answer) {
      output += `${answer.split("\n").map(escapeAnswer).join("  \n")}\n\n`;
    }
  }
  return output.trimEnd() + "\n";
}

export async function callCore(operation, { source, saved_questions = [] } = {}) {
  if (!["analyze", "reconcile"].includes(operation)) {
    throw new Error(`Unsupported core operation ${operation}`);
  }
  const template = JSON.parse(source);
  if (template?.schema_version !== 1 || !Array.isArray(template.questions)
      || !template.questions.length || !Array.isArray(template.parts)) {
    throw new Error("Compiled template format is invalid; rebuild the static site");
  }
  const questions = structuredClone(template.questions);
  const reconciliation = { exact: 0, fallback: 0, unmatched: 0, orphaned: [] };
  if (operation === "reconcile") {
    const exact = new Map(saved_questions.map((question) => [question.id, question]));
    const currentLabels = new Map();
    const savedLabels = new Map();
    for (const question of questions) {
      const label = fallbackLabel(question);
      currentLabels.set(label, (currentLabels.get(label) || 0) + 1);
    }
    for (const question of saved_questions) {
      const label = fallbackLabel(question);
      savedLabels.set(label, [...(savedLabels.get(label) || []), question]);
    }
    const consumed = new Set();
    for (const question of questions) {
      const label = fallbackLabel(question);
      const saved = exact.get(question.id);
      if (saved && !consumed.has(saved)) {
        question.answer = String(saved.answer ?? "");
        question.marked_answered = saved.marked_answered === true;
        question.skipped = saved.skipped === true;
        consumed.add(saved);
        reconciliation.exact += 1;
        continue;
      }
      if (currentLabels.get(label) === 1 && savedLabels.get(label)?.length === 1 && !consumed.has(savedLabels.get(label)[0])) {
        const fallback = savedLabels.get(label)[0];
        question.answer = String(fallback.answer ?? "");
        question.marked_answered = fallback.marked_answered === true;
        question.skipped = fallback.skipped === true;
        consumed.add(fallback);
        reconciliation.fallback += 1;
      }
    }
    for (const saved of saved_questions) {
      if (!consumed.has(saved)) {
        reconciliation.unmatched += 1;
        reconciliation.orphaned.push({
          id: String(saved.id || ""),
          fallback_id: String(saved.fallback_id || ""),
          label: String(saved.label || fallbackLabel(saved) || ""),
          breadcrumb: Array.isArray(saved.breadcrumb) ? saved.breadcrumb.map(String) : [],
          answer: String(saved.answer ?? ""),
        });
      }
    }
  }
  const markdown = template.parts.map((part) => {
    if (part.question === undefined) {
      return part.text || "";
    }
    const answer = String(questions[part.question]?.answer || "").replace(/\r\n?/gu, "\n");
    if (!answer) {
      return "";
    }
    return " " + answer.split("\n").map(escapeAnswer).join(part.continuation);
  }).join("").replace(/[ \t\n]+$/u, "") + "\n";
  return {
    template_name: template.template_name,
    fingerprint: template.fingerprint,
    questions,
    sections: sectionsFor(questions),
    markdown,
    reconciliation,
  };
}
