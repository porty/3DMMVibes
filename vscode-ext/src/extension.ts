import * as vscode from 'vscode';
import { execFile } from 'child_process';
import { promisify } from 'util';

const execFileAsync = promisify(execFile);

interface ChunkyChunk {
	ctg: string;
	cno: number;
	offset: number;
	size: number;
	flags: string;
	children: number;
	kids?: string[];
	name?: string;
}

interface ChunkyFile {
	creator: string;
	verCur: number;
	verBack: number;
	totalChunks: number;
	chunks: ChunkyChunk[];
}

class ChunkyDocument implements vscode.CustomDocument {
	readonly uri: vscode.Uri;
	constructor(uri: vscode.Uri) {
		this.uri = uri;
	}
	dispose(): void {}
}

class ChunkyEditorProvider implements vscode.CustomReadonlyEditorProvider<ChunkyDocument> {

	static register(context: vscode.ExtensionContext): vscode.Disposable {
		return vscode.window.registerCustomEditorProvider(
			'3dmm.chunkyViewer',
			new ChunkyEditorProvider(),
			{ supportsMultipleEditorsPerDocument: true }
		);
	}

	openCustomDocument(uri: vscode.Uri): ChunkyDocument {
		return new ChunkyDocument(uri);
	}

	async resolveCustomEditor(
		document: ChunkyDocument,
		webviewPanel: vscode.WebviewPanel,
	): Promise<void> {
		webviewPanel.webview.options = { enableScripts: false };
		webviewPanel.webview.html = this.loadingHtml();

		try {
			const data = await this.loadChunkyData(document.uri.fsPath);
			webviewPanel.webview.html = this.renderHtml(document.uri.fsPath, data);
		} catch (err) {
			webviewPanel.webview.html = this.errorHtml(String(err));
		}
	}

	private async loadChunkyData(filePath: string): Promise<ChunkyFile> {
		const { stdout } = await execFileAsync('3dmm', ['chunky', 'list', '--json', filePath], {
			timeout: 10_000,
		});
		return JSON.parse(stdout) as ChunkyFile;
	}

	private loadingHtml(): string {
		return `<!DOCTYPE html><html><body><p>Loading…</p></body></html>`;
	}

	private errorHtml(message: string): string {
		const escaped = message.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
		return `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family:monospace;padding:1em;color:#c00">
<strong>Error loading chunky file</strong><br><br>
<pre>${escaped}</pre>
<p>Make sure the <code>3dmm</code> binary is installed and available in PATH.</p>
</body>
</html>`;
	}

	private renderHtml(filePath: string, data: ChunkyFile): string {
		const fileName = filePath.split(/[\\/]/).pop() ?? filePath;

		const rows = data.chunks.map(c => {
			const cno = `0x${c.cno.toString(16).toUpperCase().padStart(8, '0')}`;
			const offset = `0x${c.offset.toString(16).toUpperCase().padStart(8, '0')}`;
			const kids = c.kids && c.kids.length > 0 ? c.kids.join(', ') : '';
			const name = c.name ?? '';
			return `<tr>
				<td>${esc(c.ctg)}</td>
				<td class="mono">${esc(cno)}</td>
				<td class="mono">${esc(offset)}</td>
				<td class="num">${c.size}</td>
				<td>${esc(c.flags)}</td>
				<td class="num">${c.children}</td>
				<td>${esc(kids)}</td>
				<td>${esc(name)}</td>
			</tr>`;
		}).join('\n');

		const filtered = data.totalChunks !== data.chunks.length
			? `<p class="note">Showing ${data.chunks.length} of ${data.totalChunks} chunks (filtered)</p>`
			: '';

		return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
<style>
  body {
    font-family: var(--vscode-font-family, monospace);
    font-size: var(--vscode-font-size, 13px);
    color: var(--vscode-foreground);
    background: var(--vscode-editor-background);
    padding: 12px 16px;
    margin: 0;
  }
  h1 {
    font-size: 1.1em;
    margin: 0 0 4px 0;
    font-weight: 600;
  }
  .meta {
    font-size: 0.9em;
    color: var(--vscode-descriptionForeground);
    margin-bottom: 12px;
  }
  table {
    border-collapse: collapse;
    width: 100%;
    font-family: monospace;
    font-size: 0.9em;
  }
  th {
    text-align: left;
    padding: 4px 10px;
    background: var(--vscode-editor-lineHighlightBackground, rgba(128,128,128,0.1));
    border-bottom: 1px solid var(--vscode-panel-border, #555);
    white-space: nowrap;
    color: var(--vscode-foreground);
  }
  td {
    padding: 2px 10px;
    border-bottom: 1px solid var(--vscode-panel-border, rgba(128,128,128,0.15));
    white-space: nowrap;
    vertical-align: top;
  }
  tr:hover td {
    background: var(--vscode-list-hoverBackground, rgba(128,128,128,0.1));
  }
  .num { text-align: right; }
  .mono { font-family: monospace; }
  .note {
    font-size: 0.85em;
    color: var(--vscode-descriptionForeground);
    margin-top: 8px;
  }
</style>
</head>
<body>
<h1>${esc(fileName)}</h1>
<p class="meta">
  Creator: <strong>${esc(data.creator)}</strong> &nbsp;|&nbsp;
  Version: ${data.verCur}/${data.verBack} &nbsp;|&nbsp;
  Chunks: ${data.totalChunks}
</p>
<table>
  <thead>
    <tr>
      <th>CTG</th>
      <th>CNO</th>
      <th>Offset</th>
      <th>Size</th>
      <th>Flags</th>
      <th>Children</th>
      <th>Child Types</th>
      <th>Name</th>
    </tr>
  </thead>
  <tbody>
${rows}
  </tbody>
</table>
${filtered}
</body>
</html>`;
	}
}

function esc(s: string): string {
	return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function activate(context: vscode.ExtensionContext) {
	context.subscriptions.push(ChunkyEditorProvider.register(context));
}

export function deactivate() {}
