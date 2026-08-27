const storageKey = "read-run";
const state = {
  courses: [], course: null, lessons: [], current: 0, lesson: null, lab: null,
  passed: new Set(JSON.parse(localStorage.getItem(`${storageKey}:passed`) || "[]")),
};

let editor;
let labPoll;
let sqlLintTimer;
let sqlLintVersion = 0;
const elements = {
  chapter: document.querySelector("#chapter"), title: document.querySelector("#lesson-title"),
  instruction: document.querySelector("#instruction"), editor: document.querySelector("#editor"),
  plainEditor: document.querySelector("#plain-editor"), editorLabel: document.querySelector("#editor-label"),
  workspace: document.querySelector(".workspace"), output: document.querySelector("#output"),
  status: document.querySelector("#status"), progress: document.querySelector("#progress"),
  chapterPicker: document.querySelector("#chapter-picker"), lessonPicker: document.querySelector("#lesson-picker"),
  courseOutline: document.querySelector("#course-outline"), courseSummary: document.querySelector("#course-summary"),
  courseTitle: document.querySelector("#course-title"), mapToggle: document.querySelector("#map-toggle"),
  mapClose: document.querySelector("#map-close"), run: document.querySelector("#run"),
  previous: document.querySelector("#previous"), next: document.querySelector("#next"),
  format: document.querySelector("#format"), lintStatus: document.querySelector("#lint-status"),
  mapBackdrop: document.querySelector("#map-backdrop"), labPanel: document.querySelector("#lab-panel"),
  labState: document.querySelector("#lab-state"), labMessage: document.querySelector("#lab-message"),
  labSteps: document.querySelector("#lab-steps"), labLinks: document.querySelector("#lab-links"),
  labCredentials: document.querySelector("#lab-credentials"), labRetry: document.querySelector("#lab-retry"),
};

async function start() {
  editor = window.createGoEditor({
    parent: elements.editor,
    onRun: run,
    onDiagnostics: (diagnostics) => {
      if ((state.lesson?.mode || "go") === "go") updateLintStatus(diagnostics);
    },
  });
  state.courses = await getJSON("/api/courses");
  const requested = new URLSearchParams(location.search).get("course") || "learn-go";
  state.course = state.courses.find((item) => item.id === requested) || state.courses[0];
  state.lessons = await getJSON(`/api/lessons?course=${encodeURIComponent(state.course.id)}`);
  elements.courseTitle.textContent = state.course.title;
  populateChapters();
  const savedID = localStorage.getItem(`${storageKey}:current:${state.course.id}`);
  const savedIndex = state.lessons.findIndex((item) => item.id === savedID);
  state.current = savedIndex >= 0 ? savedIndex : 0;
  await loadLesson();
  if (state.course.requiredLab) await startLab();
}

async function loadLesson() {
  const summary = state.lessons[state.current];
  state.lesson = await getJSON(`/api/lessons/${summary.id}`);
  localStorage.setItem(`${storageKey}:current:${state.course.id}`, summary.id);
  elements.chapter.textContent = state.lesson.chapter;
  elements.title.textContent = state.lesson.title;
  elements.instruction.textContent = state.lesson.instruction;
  configureWorkspace();
  setEditorValue(localStorage.getItem(`${storageKey}:draft:${summary.id}`) || state.lesson.starter);
  elements.output.textContent = state.lesson.mode === "lab" ? "Complete the task in Grafana, then run Check." : "Output will appear here.";
  setStatus(state.passed.has(summary.id) ? "passed" : "idle");
  updatePickers();
  updateNavigation();
  renderOutline();
  updateRunAvailability();
  scheduleSQLLint();
  focusEditor();
}

function configureWorkspace() {
  const mode = state.lesson.mode || "go";
  elements.workspace.dataset.mode = mode;
  elements.editorLabel.textContent = state.lesson.editorLabel || (mode === "go" ? "main.go" : "lesson.txt");
  elements.editor.hidden = mode !== "go";
  elements.plainEditor.hidden = mode === "go" || mode === "lab";
  elements.format.hidden = mode !== "go";
  elements.lintStatus.hidden = mode !== "go" && mode !== "sql";
  if (mode === "go" || mode === "sql") {
    elements.lintStatus.className = "lint-status";
    elements.lintStatus.textContent = "CHECKING";
  }
  elements.run.textContent = { go: "Run", sql: "Run SQL", promql: "Run Query", file: "Apply File", lab: "Check" }[mode] || "Run";
}

function getEditorValue() {
  if ((state.lesson.mode || "go") === "go") return editor.getValue();
  if (state.lesson.mode === "lab") return "check";
  return elements.plainEditor.value;
}

function setEditorValue(value) {
  if ((state.lesson.mode || "go") === "go") editor.setValue(value);
  else elements.plainEditor.value = value;
}

function focusEditor() {
  if ((state.lesson.mode || "go") === "go") editor.focus();
  else if (state.lesson.mode !== "lab") elements.plainEditor.focus();
}

function populateChapters() {
  const chapters = [...new Set(state.lessons.map((item) => item.chapter))];
  elements.chapterPicker.replaceChildren(...chapters.map((chapter) => {
    const option = document.createElement("option");
    option.value = chapter;
    option.textContent = chapter.replace(/^Chapter (\d+) · /, "CH$1: ");
    return option;
  }));
}

function updatePickers() {
  const currentChapter = state.lessons[state.current].chapter;
  elements.chapterPicker.value = currentChapter;
  const lessonOptions = state.lessons.map((item, index) => ({ item, index }))
    .filter(({ item }) => item.chapter === currentChapter)
    .map(({ item, index }, chapterIndex) => {
      const option = document.createElement("option");
      option.value = index;
      option.textContent = `L${chapterIndex + 1}: ${item.title}`;
      return option;
    });
  elements.lessonPicker.replaceChildren(...lessonOptions);
  elements.lessonPicker.value = state.current;
}

function renderOutline() {
  const validPassed = state.lessons.filter((item) => state.passed.has(item.id));
  elements.courseSummary.textContent = `${validPassed.length} of ${state.lessons.length} lessons complete`;
  const groups = [];
  for (const [index, item] of state.lessons.entries()) {
    let group = groups.at(-1);
    if (!group || group.chapter !== item.chapter) {
      group = { chapter: item.chapter, lessons: [] };
      groups.push(group);
    }
    group.lessons.push({ ...item, index });
  }
  const currentChapter = state.lessons[state.current].chapter;
  const fragment = document.createDocumentFragment();
  for (const group of groups) {
    const complete = group.lessons.filter(({ id }) => state.passed.has(id)).length;
    const details = document.createElement("details");
    details.className = "outline-chapter";
    details.open = group.chapter === currentChapter;
    const summary = document.createElement("summary");
    const heading = document.createElement("span");
    heading.className = "outline-chapter-title";
    heading.textContent = group.chapter.replace(/^Chapter (\d+) · /, "CH$1 · ");
    const count = document.createElement("span");
    count.className = "outline-count";
    count.textContent = `${complete}/${group.lessons.length}`;
    const progress = document.createElement("span");
    progress.className = "outline-progress";
    const progressValue = document.createElement("span");
    progressValue.style.width = `${complete / group.lessons.length * 100}%`;
    progress.append(progressValue);
    summary.append(heading, count, progress);
    const list = document.createElement("ol");
    list.className = "outline-lessons";
    group.lessons.forEach((item, chapterIndex) => {
      const row = document.createElement("li");
      const button = document.createElement("button");
      const passed = state.passed.has(item.id);
      const current = item.index === state.current;
      button.type = "button";
      button.className = `outline-lesson${passed ? " completed" : ""}${current ? " current" : ""}`;
      if (current) button.setAttribute("aria-current", "step");
      const marker = document.createElement("span");
      marker.className = "lesson-marker";
      marker.textContent = passed ? "✓" : String(chapterIndex + 1);
      marker.setAttribute("aria-hidden", "true");
      const title = document.createElement("span");
      title.textContent = item.title;
      button.append(marker, title);
      button.addEventListener("click", async () => {
        state.current = item.index;
        closeCourseMap();
        await loadLesson();
      });
      row.append(button);
      list.append(row);
    });
    details.append(summary, list);
    fragment.append(details);
  }
  elements.courseOutline.replaceChildren(fragment);
  requestAnimationFrame(() => elements.courseOutline.querySelector("[aria-current='step']")?.scrollIntoView({ block: "nearest" }));
}

async function startLab() {
  clearTimeout(labPoll);
  elements.labPanel.hidden = false;
  elements.labRetry.hidden = true;
  try {
    state.lab = await getJSON(`/api/labs/${encodeURIComponent(state.course.requiredLab)}/start`, { method: "POST" });
    renderLab();
    pollLab();
  } catch (error) {
    renderLabError(error.message);
  }
}

async function pollLab() {
  if (!state.course.requiredLab) return;
  try {
    state.lab = await getJSON(`/api/labs/${encodeURIComponent(state.course.requiredLab)}`);
    renderLab();
    if (state.lab.state === "starting") labPoll = setTimeout(pollLab, 1000);
  } catch (error) {
    renderLabError(error.message);
  }
}

function renderLab() {
  const labels = { stopped: "Lab stopped", starting: "Starting services…", ready: "Lab ready", error: "Lab needs attention" };
  elements.labPanel.dataset.state = state.lab.state;
  elements.labState.textContent = labels[state.lab.state] || state.lab.state;
  elements.labMessage.textContent = state.lab.message;
  elements.labSteps.replaceChildren(...state.lab.steps.map((step) => {
    const item = document.createElement("li");
    item.className = step.ready ? "ready" : "waiting";
    item.textContent = `${step.ready ? "✓" : "·"} ${step.name}`;
    return item;
  }));
  elements.labLinks.replaceChildren(...state.lab.links.map((link) => {
    const anchor = document.createElement("a");
    anchor.href = link.url;
    anchor.target = "_blank";
    anchor.rel = "noreferrer";
    anchor.textContent = `Open ${link.label}`;
    return anchor;
  }));
  const credentials = state.lab.credentials;
  elements.labCredentials.textContent = `Grafana login: ${credentials.username} / ${credentials.password}`;
  elements.labRetry.hidden = state.lab.state !== "error" && state.lab.state !== "stopped";
  updateRunAvailability();
}

function renderLabError(message) {
  state.lab = { state: "error" };
  elements.labPanel.dataset.state = "error";
  elements.labState.textContent = "Lab needs attention";
  elements.labMessage.textContent = message;
  elements.labRetry.hidden = false;
  updateRunAvailability();
}

function updateRunAvailability() {
  elements.run.disabled = Boolean(state.course?.requiredLab && state.lab?.state !== "ready");
}

async function run() {
  if (state.course.requiredLab && state.lab?.state !== "ready") return;
  elements.run.disabled = true;
  elements.output.textContent = state.lesson.mode === "lab" ? "Checking the live lab…" : "Running…";
  setStatus("running");
  const input = getEditorValue();
  if (state.lesson.mode !== "lab") localStorage.setItem(`${storageKey}:draft:${state.lesson.id}`, input);
  try {
    const result = await getJSON(`/api/lessons/${state.lesson.id}/run`, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ input }),
    });
    elements.output.textContent = result.output || "(no output)";
    if (result.passed) {
      state.passed.add(state.lesson.id);
      localStorage.setItem(`${storageKey}:passed`, JSON.stringify([...state.passed]));
      setStatus("passed");
      renderOutline();
    } else setStatus("failed");
    updateNavigation();
  } catch (error) {
    elements.output.textContent = error.message;
    setStatus("failed");
  } finally {
    updateRunAvailability();
  }
}

async function formatEditor() {
  if ((state.lesson.mode || "go") !== "go") return;
  elements.format.disabled = true;
  try {
    const result = await getJSON("/api/format", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ input: editor.getValue() }),
    });
    editor.setValue(result.code);
    localStorage.setItem(`${storageKey}:draft:${state.lesson.id}`, result.code);
    editor.focus();
  } catch (error) {
    elements.output.textContent = error.message;
  } finally {
    elements.format.disabled = false;
  }
}

function updateLintStatus(diagnostics) {
  if (!state.lesson || !["go", "sql"].includes(state.lesson.mode || "go")) return;
  const errors = diagnostics.filter((item) => item.severity === "error").length;
  const warnings = diagnostics.length - errors;
  elements.lintStatus.className = `lint-status ${errors ? "error" : warnings ? "warning" : "ok"}`;
  elements.lintStatus.textContent = errors ? `${errors} ERROR${errors === 1 ? "" : "S"}` : warnings ? `${warnings} WARNING${warnings === 1 ? "" : "S"}` : "LINT OK";
  elements.lintStatus.title = diagnostics.map((item) => `L${item.line}:${item.column} ${item.message}`).join("\n");
  if (state.lesson.mode === "sql") {
    if (diagnostics.length) elements.output.textContent = `SQLite lint\n${elements.lintStatus.title}`;
    else if (elements.output.textContent.startsWith("SQLite lint")) elements.output.textContent = "SQL syntax is valid. Run it to check the result.";
  }
}

function scheduleSQLLint() {
  clearTimeout(sqlLintTimer);
  const version = ++sqlLintVersion;
  if (state.lesson?.mode !== "sql") return;
  const input = elements.plainEditor.value;
  if (!input.trim()) {
    updateLintStatus([{ line: 1, column: 1, severity: "error", message: "SQL is empty" }]);
    return;
  }
  elements.lintStatus.className = "lint-status";
  elements.lintStatus.textContent = "CHECKING";
  sqlLintTimer = setTimeout(async () => {
    try {
      const diagnostics = await getJSON(`/api/lessons/${state.lesson.id}/lint`, {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ input }),
      });
      if (version === sqlLintVersion) updateLintStatus(diagnostics);
    } catch (error) {
      if (version === sqlLintVersion) updateLintStatus([{ line: 1, column: 1, severity: "error", message: error.message }]);
    }
  }, 350);
}

function setStatus(value) {
  const labels = { idle: "NOT RUN", running: "RUNNING", passed: "PASS", failed: "FAIL" };
  elements.status.className = `status ${value}`;
  elements.status.textContent = labels[value];
}

function updateNavigation() {
  elements.progress.textContent = `${state.current + 1} / ${state.lessons.length}`;
  elements.previous.disabled = state.current === 0;
  elements.next.disabled = state.current === state.lessons.length - 1 || !state.passed.has(state.lesson.id);
}

async function move(offset) { state.current += offset; await loadLesson(); }
function closeCourseMap() {
  document.body.classList.remove("map-open");
  elements.mapToggle.setAttribute("aria-expanded", "false");
}
async function getJSON(url, options) {
  const response = await fetch(url, options);
  if (!response.ok) throw new Error((await response.text()) || `Request failed: ${response.status}`);
  return response.json();
}

elements.run.addEventListener("click", run);
elements.format.addEventListener("click", formatEditor);
elements.labRetry.addEventListener("click", startLab);
elements.plainEditor.addEventListener("keydown", (event) => {
  if (event.ctrlKey && event.key === "Enter") { event.preventDefault(); run(); }
});
elements.plainEditor.addEventListener("input", scheduleSQLLint);
elements.previous.addEventListener("click", () => move(-1));
elements.next.addEventListener("click", () => move(1));
elements.mapToggle.addEventListener("click", () => {
  const open = document.body.classList.toggle("map-open");
  elements.mapToggle.setAttribute("aria-expanded", String(open));
});
elements.mapClose.addEventListener("click", closeCourseMap);
elements.mapBackdrop.addEventListener("click", closeCourseMap);
document.addEventListener("keydown", (event) => { if (event.key === "Escape") closeCourseMap(); });
elements.chapterPicker.addEventListener("change", async () => {
  state.current = state.lessons.findIndex((item) => item.chapter === elements.chapterPicker.value);
  await loadLesson();
});
elements.lessonPicker.addEventListener("change", async () => {
  state.current = Number(elements.lessonPicker.value);
  await loadLesson();
});
start().catch((error) => {
  elements.title.textContent = "Could not load lessons";
  elements.instruction.textContent = error.message;
});
