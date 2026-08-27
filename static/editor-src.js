import { basicSetup } from "codemirror";
import { EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { go } from "@codemirror/lang-go";
import { linter, lintGutter } from "@codemirror/lint";
import { oneDark } from "@codemirror/theme-one-dark";

window.createGoEditor = ({ parent, onRun, onDiagnostics }) => {
  const goLinter = linter(async (view) => {
    const code = view.state.doc.toString();
    if (!code.trim()) {
      onDiagnostics([]);
      return [];
    }

    try {
      const response = await fetch("/api/lint", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code }),
      });
      if (!response.ok) throw new Error(await response.text());
      const diagnostics = await response.json();
      onDiagnostics(diagnostics);
      return diagnostics.map((item) => {
        const line = view.state.doc.line(Math.min(Math.max(item.line, 1), view.state.doc.lines));
        const from = Math.min(line.to, line.from + Math.max(item.column - 1, 0));
        return {
          from,
          to: Math.min(line.to, from + 1),
          severity: item.severity,
          message: item.message,
        };
      });
    } catch (error) {
      const diagnostics = [{ severity: "error" }];
      onDiagnostics(diagnostics);
      return [{ from: 0, to: 0, severity: "error", message: `Lint service unavailable: ${error.message}` }];
    }
  }, { delay: 450 });

  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: "",
      extensions: [
        keymap.of([{
          key: "Ctrl-Enter",
          run: () => {
            onRun();
            return true;
          },
        }]),
        basicSetup,
        go(),
        oneDark,
        lintGutter(),
        goLinter,
        EditorView.contentAttributes.of({ "aria-label": "Go code" }),
      ],
    }),
  });

  return {
    getValue: () => view.state.doc.toString(),
    setValue: (value) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } }),
    focus: () => view.focus(),
  };
};
