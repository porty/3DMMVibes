import * as vscode from 'vscode';
import { execFile } from 'child_process';
import { promisify } from 'util';

const execFileAsync = promisify(execFile);

const PAGE_SIZE = 500;

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
	filteredTotal: number;
	offset: number;
	limit: number;
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
		webviewPanel.webview.options = { enableScripts: true };
		webviewPanel.webview.html = this.loadingHtml();

		const filePath = document.uri.fsPath;

		const loadPage = async (page: number) => {
			const offset = page * PAGE_SIZE;
			try {
				const data = await this.fetchPage(filePath, offset);
				webviewPanel.webview.html = this.renderHtml(filePath, data, page);
			} catch (err) {
				webviewPanel.webview.html = this.errorHtml(String(err));
			}
		};

		webviewPanel.webview.onDidReceiveMessage((msg: { type: string; page: number }) => {
			if (msg.type === 'navigate') {
				loadPage(msg.page);
			}
		});

		await loadPage(0);
	}

	private async fetchPage(filePath: string, offset: number): Promise<ChunkyFile> {
		const { stdout } = await execFileAsync(
			'3dmm',
			['chunky', 'list', '--json', '--limit', String(PAGE_SIZE), '--offset', String(offset), filePath],
			{ maxBuffer: 16 * 1024 * 1024 }
		);
		return JSON.parse(stdout) as ChunkyFile;
	}

	private loadingHtml(): string {
		return `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:1em"><p>Loading…</p></body></html>`;
	}

	private errorHtml(message: string): string {
		const escaped = esc(message);
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

	private renderHtml(filePath: string, data: ChunkyFile, page: number): string {
		const fileName = filePath.split(/[\\/]/).pop() ?? filePath;
		const totalPages = Math.ceil(data.filteredTotal / PAGE_SIZE);
		const isFiltered = data.filteredTotal !== data.totalChunks;

		const rows = data.chunks.map(c => {
			const cno = `0x${c.cno.toString(16).toUpperCase().padStart(8, '0')}`;
			const offset = `0x${c.offset.toString(16).toUpperCase().padStart(8, '0')}`;
			const kids = c.kids && c.kids.length > 0 ? c.kids.join(', ') : '';
			return `<tr>
				<td>${esc(c.ctg)}</td>
				<td class="mono">${esc(cno)}</td>
				<td class="mono">${esc(offset)}</td>
				<td class="num">${c.size}</td>
				<td>${esc(c.flags)}</td>
				<td class="num">${c.children}</td>
				<td>${esc(kids)}</td>
				<td>${esc(c.name ?? '')}</td>
			</tr>`;
		}).join('\n');

		const chunkCountLabel = isFiltered
			? `${data.filteredTotal} chunks (filtered from ${data.totalChunks})`
			: `${data.totalChunks} chunks`;

		const pageInfo = totalPages > 1
			? `Page ${page + 1} of ${totalPages} &nbsp;(${PAGE_SIZE} per page)`
			: '';

		const prevDisabled = page <= 0 ? 'disabled' : '';
		const nextDisabled = page >= totalPages - 1 ? 'disabled' : '';

		const pagination = totalPages > 1 ? `
		<div class="pagination">
			<button onclick="navigate(${page - 1})" ${prevDisabled}>&#8592; Prev</button>
			<span>${pageInfo}</span>
			<button onclick="navigate(${page + 1})" ${nextDisabled}>Next &#8594;</button>
		</div>` : '';

		return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
<style>
  body {
    font-family: var(--vscode-font-family, monospace);
    font-size: var(--vscode-font-size, 13px);
    color: var(--vscode-foreground);
    background: var(--vscode-editor-background);
    padding: 12px 16px;
    margin: 0;
  }
  h1 { font-size: 1.1em; margin: 0 0 4px 0; font-weight: 600; }
  .meta {
    font-size: 0.9em;
    color: var(--vscode-descriptionForeground);
    margin-bottom: 10px;
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
  }
  td {
    padding: 2px 10px;
    border-bottom: 1px solid var(--vscode-panel-border, rgba(128,128,128,0.15));
    white-space: nowrap;
    vertical-align: top;
  }
  tr:hover td { background: var(--vscode-list-hoverBackground, rgba(128,128,128,0.1)); }
  .num { text-align: right; }
  .mono { font-family: monospace; }
  .pagination {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 10px;
    font-size: 0.9em;
  }
  button {
    background: var(--vscode-button-background);
    color: var(--vscode-button-foreground);
    border: none;
    padding: 4px 10px;
    cursor: pointer;
    border-radius: 2px;
    font-size: 0.9em;
  }
  button:hover:not(:disabled) { background: var(--vscode-button-hoverBackground); }
  button:disabled { opacity: 0.4; cursor: default; }
</style>
</head>
<body>
<h1>${esc(fileName)}</h1>
<p class="meta">
  Creator: <strong>${esc(data.creator)}</strong> &nbsp;|&nbsp;
  Version: ${data.verCur}/${data.verBack} &nbsp;|&nbsp;
  ${chunkCountLabel}
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
${pagination}
<script>
  const vscode = acquireVsCodeApi();
  function navigate(page) {
    vscode.postMessage({ type: 'navigate', page });
  }
</script>
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
