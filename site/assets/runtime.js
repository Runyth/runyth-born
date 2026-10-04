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
  // Answers are literal text, including punctuation starting continuation lines.
  return String(value).replace(/[\\`*_{}\[\]()<>!#~+\-=.|]/gu, "\\$&");
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
    for (const question of questions) {
      const label = fallbackLabel(question);
      const saved = exact.get(question.id)
        || (currentLabels.get(label) === 1 && savedLabels.get(label)?.length === 1
          ? savedLabels.get(label)[0] : undefined);
      if (saved) {
        question.answer = String(saved.answer ?? "");
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
  };
}
