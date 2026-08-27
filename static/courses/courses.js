const storageKey = "read-run";
const passed = new Set(JSON.parse(localStorage.getItem(`${storageKey}:passed`) || "[]"));
const grid = document.querySelector("#course-grid");

async function start() {
  const courses = await getJSON("/api/courses");
  const cards = await Promise.all(courses.map(async (course) => {
    const lessons = await getJSON(`/api/lessons?course=${encodeURIComponent(course.id)}`);
    const completed = lessons.filter((lesson) => passed.has(lesson.id)).length;
    const chapters = new Set(lessons.map((lesson) => lesson.chapter)).size;
    return createCard(course, completed, chapters);
  }));
  grid.replaceChildren(...cards);
}

function createCard(course, completed, chapters) {
  const link = document.createElement("a");
  link.className = "course-card";
  link.href = `/?course=${encodeURIComponent(course.id)}`;

  const label = document.createElement("span");
  label.className = "course-card-label";
  label.textContent = course.id === "grafana" ? "LIVE LAB COURSE" : course.id === "sql" ? "SQL COURSE" : "GO COURSE";

  const title = document.createElement("h2");
  title.textContent = course.title;

  const description = document.createElement("p");
  description.textContent = course.description;

  const meta = document.createElement("div");
  meta.className = "course-card-meta";
  meta.textContent = `${chapters} chapters · ${course.lessonCount} lessons`;

  const progress = document.createElement("div");
  progress.className = "course-card-progress";
  const bar = document.createElement("span");
  bar.style.width = `${completed / course.lessonCount * 100}%`;
  progress.append(bar);

  const footer = document.createElement("div");
  footer.className = "course-card-footer";
  footer.innerHTML = `<span>${completed} complete</span><strong>${completed ? "Continue →" : "Start →"}</strong>`;

  link.append(label, title, description, meta, progress, footer);
  return link;
}

async function getJSON(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

start().catch((error) => {
  grid.textContent = `Could not load courses: ${error.message}`;
});
