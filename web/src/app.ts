interface Session {
  id: string;
  name?: string;
  model: string;
  task: string;
  cwd: string;
  started_at: string;
  status: 'running' | 'done';
  turns: number;
  total_cost: number;
  last_state?: string;
  last_detail?: string;
}

// Global state
let sessions: Session[] = [];
let selectedSessionId: string | null = null;
let activeFilter: 'all' | 'running' | 'done' = 'all';
let searchQuery = '';
let eventSource: EventSource | null = null;
let isFollowingLogs = true;
let isTaskCollapsed = false;

// DOM Cache
const sessionsListEl = document.getElementById('sessions-list') as HTMLDivElement;
const searchInput = document.getElementById('search-input') as HTMLInputElement;
const filterAllBtn = document.getElementById('filter-all') as HTMLButtonElement;
const filterRunningBtn = document.getElementById('filter-running') as HTMLButtonElement;
const filterDoneBtn = document.getElementById('filter-done') as HTMLButtonElement;
const refreshSessionsBtn = document.getElementById('refresh-sessions') as HTMLButtonElement;

const emptyStateEl = document.getElementById('empty-state') as HTMLDivElement;
const detailContentEl = document.getElementById('detail-content') as HTMLDivElement;

const detailIdEl = document.getElementById('detail-id') as HTMLSpanElement;
const detailModelEl = document.getElementById('detail-model') as HTMLSpanElement;
const detailStatusEl = document.getElementById('detail-status') as HTMLSpanElement;
const detailNameEl = document.getElementById('detail-name') as HTMLDivElement;
const detailCwdEl = document.getElementById('detail-cwd') as HTMLDivElement;
const detailCostEl = document.getElementById('detail-cost') as HTMLDivElement;
const detailTurnsEl = document.getElementById('detail-turns') as HTMLDivElement;
const detailAgeEl = document.getElementById('detail-age') as HTMLDivElement;
const killSessionBtn = document.getElementById('kill-session-btn') as HTMLButtonElement;

const toggleTaskBtn = document.getElementById('toggle-task-btn') as HTMLDivElement;
const taskChevron = document.getElementById('task-chevron') as HTMLElement;
const taskContentWrapper = document.getElementById('task-content-wrapper') as HTMLDivElement;

const logsContainer = document.getElementById('logs-container') as HTMLDivElement;
const followScrollBtn = document.getElementById('follow-scroll-btn') as HTMLButtonElement;

// Render State Machine for Log Stream
interface ActiveRenderState {
  currentTurnNum: number;
  currentTurnEl: HTMLDivElement | null;
  currentAssistantMsgEl: HTMLDivElement | null;
  currentAssistantTextEl: HTMLDivElement | null;
  currentThinkingEl: HTMLDivElement | null;
  currentThinkingTextEl: HTMLDivElement | null;
  currentToolCallEl: HTMLDivElement | null;
  currentToolArgsEl: HTMLPreElement | null;
  currentToolArgsRaw: string;
  toolCallMap: Map<string, {
    container: HTMLDivElement;
    header: HTMLDivElement;
    argsPre: HTMLPreElement;
    resultEl: HTMLDivElement | null;
  }>;
}

let renderState: ActiveRenderState = createInitialRenderState();

function createInitialRenderState(): ActiveRenderState {
  return {
    currentTurnNum: 0,
    currentTurnEl: null,
    currentAssistantMsgEl: null,
    currentAssistantTextEl: null,
    currentThinkingEl: null,
    currentThinkingTextEl: null,
    currentToolCallEl: null,
    currentToolArgsEl: null,
    currentToolArgsRaw: '',
    toolCallMap: new Map(),
  };
}

// Initial initialization
document.addEventListener('DOMContentLoaded', () => {
  // Init lucide icons
  (window as any).lucide?.createIcons();

  // Attach Event Listeners
  searchInput.addEventListener('input', handleSearch);
  filterAllBtn.addEventListener('click', () => setFilter('all'));
  filterRunningBtn.addEventListener('click', () => setFilter('running'));
  filterDoneBtn.addEventListener('click', () => setFilter('done'));
  refreshSessionsBtn.addEventListener('click', () => fetchSessions(true));
  
  toggleTaskBtn.addEventListener('click', toggleTaskCollapse);
  killSessionBtn.addEventListener('click', handleKillSession);
  
  logsContainer.addEventListener('scroll', handleLogsScroll);
  followScrollBtn.addEventListener('click', enableFollowScroll);

  // Initial Fetch
  fetchSessions();
  
  // Set up auto polling for session list
  setInterval(() => {
    fetchSessions(false);
  }, 4000);
});

// Fetch sessions list from backend
async function fetchSessions(showSpinner = false) {
  if (showSpinner) {
    sessionsListEl.innerHTML = `
      <div class="flex flex-col items-center justify-center h-48 text-zinc-500 space-y-2">
        <i data-lucide="loader-2" class="h-6 w-6 animate-spin"></i>
        <span class="text-xs">Fetching sessions...</span>
      </div>
    `;
    (window as any).lucide?.createIcons();
  }

  try {
    const res = await fetch('/api/sessions');
    if (!res.ok) throw new Error('Failed to fetch sessions');
    sessions = await res.json();
    updateStats();
    renderSessionsList();
  } catch (err) {
    console.error('Error fetching sessions:', err);
    if (showSpinner) {
      sessionsListEl.innerHTML = `
        <div class="flex flex-col items-center justify-center h-48 text-red-400 p-4 text-center">
          <i data-lucide="alert-circle" class="h-6 w-6 mb-1"></i>
          <span class="text-xs font-semibold">Connection failed</span>
          <span class="text-[10px] text-zinc-500 mt-1">Make sure the Go server is running.</span>
        </div>
      `;
      (window as any).lucide?.createIcons();
    }
  }
}

// Update stats on sidebar
function updateStats() {
  const total = sessions.length;
  const running = sessions.filter(s => s.status === 'running').length;
  const totalCost = sessions.reduce((sum, s) => sum + (s.total_cost || 0), 0);

  document.getElementById('stat-total')!.textContent = total.toString();
  document.getElementById('stat-running')!.textContent = running.toString();
  document.getElementById('stat-cost')!.textContent = `$${totalCost.toFixed(3)}`;
}

// Handle filters
function setFilter(filter: 'all' | 'running' | 'done') {
  activeFilter = filter;
  
  // Update UI buttons
  const buttons = [
    { el: filterAllBtn, key: 'all' },
    { el: filterRunningBtn, key: 'running' },
    { el: filterDoneBtn, key: 'done' },
  ];
  
  buttons.forEach(btn => {
    if (btn.key === filter) {
      btn.el.className = 'flex-1 rounded px-2.5 py-1 text-xs font-semibold bg-zinc-800 text-zinc-200 hover:bg-zinc-700 transition';
    } else {
      btn.el.className = 'flex-1 rounded px-2.5 py-1 text-xs font-semibold text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200 transition';
    }
  });

  renderSessionsList();
}

function handleSearch(e: Event) {
  searchQuery = (e.target as HTMLInputElement).value.toLowerCase();
  renderSessionsList();
}

// Render the list of sessions in the sidebar
function renderSessionsList() {
  let filtered = sessions;

  // Status Filter
  if (activeFilter === 'running') {
    filtered = filtered.filter(s => s.status === 'running');
  } else if (activeFilter === 'done') {
    filtered = filtered.filter(s => s.status === 'done');
  }

  // Search Filter
  if (searchQuery.trim() !== '') {
    filtered = filtered.filter(s => 
      s.id.toLowerCase().includes(searchQuery) ||
      (s.name && s.name.toLowerCase().includes(searchQuery)) ||
      s.model.toLowerCase().includes(searchQuery) ||
      s.task.toLowerCase().includes(searchQuery)
    );
  }

  if (filtered.length === 0) {
    sessionsListEl.innerHTML = `
      <div class="flex flex-col items-center justify-center h-48 text-zinc-500 p-4 text-center">
        <i data-lucide="inbox" class="h-6 w-6 mb-1 text-zinc-600"></i>
        <span class="text-xs">No sessions found</span>
      </div>
    `;
    (window as any).lucide?.createIcons();
    return;
  }

  sessionsListEl.innerHTML = filtered.map(s => {
    const isSelected = s.id === selectedSessionId;
    const isRunning = s.status === 'running';
    
    // Status badges / colors
    const statusDot = isRunning 
      ? `<span class="relative flex h-2 w-2 mr-1.5"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span><span class="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span></span>`
      : `<span class="h-2 w-2 rounded-full bg-zinc-600 mr-1.5 inline-block"></span>`;
    
    const costStr = s.total_cost > 0 ? `$${s.total_cost.toFixed(4)}` : '';
    const turnsStr = s.turns > 0 ? `${s.turns} turns` : '';
    
    const timeStr = formatRelativeTime(new Date(s.started_at));
    const taskSnippet = s.task.length > 70 ? s.task.substring(0, 70) + '...' : s.task;
    const nameLabel = s.name ? `<div class="text-xs font-semibold text-zinc-300 truncate">${escapeHtml(s.name)}</div>` : '';

    // Color code models
    let modelBadgeClass = 'text-zinc-400 bg-zinc-850';
    if (s.model.includes('opus')) modelBadgeClass = 'text-orange-400 bg-orange-950/20 border-orange-900/30';
    else if (s.model.includes('gpt')) modelBadgeClass = 'text-emerald-400 bg-emerald-950/20 border-emerald-900/30';
    else if (s.model.includes('gemini')) modelBadgeClass = 'text-cyan-400 bg-cyan-950/20 border-cyan-900/30';

    return `
      <div 
        class="p-4 cursor-pointer flex flex-col space-y-2 transition duration-150 border-l-2 select-none ${
          isSelected 
            ? 'bg-zinc-800/40 border-emerald-500' 
            : 'hover:bg-zinc-800/20 border-transparent'
        }"
        onclick="selectSession('${s.id}')"
      >
        <div class="flex items-center justify-between">
          <span class="font-mono text-xs font-bold ${isSelected ? 'text-emerald-400' : 'text-zinc-400'}">${s.id}</span>
          <span class="text-[10px] text-zinc-500">${timeStr}</span>
        </div>
        ${nameLabel}
        <div class="text-xs text-zinc-400 line-clamp-2 pr-1 select-text">${escapeHtml(taskSnippet)}</div>
        <div class="flex items-center justify-between pt-1 text-[10px]">
          <span class="flex items-center text-zinc-400 font-medium">
            ${statusDot}
            ${isRunning ? `<span class="text-emerald-400 font-semibold">${s.last_state || 'running'}</span>` : 'done'}
          </span>
          <div class="flex items-center space-x-2">
            <span class="px-1.5 py-0.5 rounded border border-zinc-800 text-zinc-400 font-mono text-[9px] ${modelBadgeClass}">${escapeHtml(s.model)}</span>
            <span class="text-zinc-500">${turnsStr}</span>
            <span class="text-zinc-400 font-semibold">${costStr}</span>
          </div>
        </div>
      </div>
    `;
  }).join('');

  (window as any).lucide?.createIcons();
}

// Action: Click a session in the sidebar
(window as any).selectSession = function(id: string) {
  if (id === selectedSessionId) return;
  selectedSessionId = id;
  
  // Update selected highlight in sidebar
  renderSessionsList();
  
  // Close existing event source if active
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }

  emptyStateEl.classList.add('hidden');
  detailContentEl.classList.remove('hidden');

  const s = sessions.find(sess => sess.id === id);
  if (!s) return;

  // Reset viewport state
  renderState = createInitialRenderState();
  isFollowingLogs = true;
  updateFollowScrollButton();
  
  // Update header metadata
  detailIdEl.textContent = s.id;
  detailModelEl.textContent = s.model;
  detailNameEl.textContent = s.name || '';
  if (s.name) {
    detailNameEl.classList.remove('hidden');
  } else {
    detailNameEl.classList.add('hidden');
  }
  
  detailCwdEl.querySelector('span')!.textContent = s.cwd;
  detailCwdEl.title = s.cwd;
  
  detailCostEl.textContent = s.total_cost > 0 ? `$${s.total_cost.toFixed(4)}` : '$0.0000';
  detailTurnsEl.textContent = s.turns > 0 ? s.turns.toString() : '0';
  detailAgeEl.textContent = formatDuration(new Date(s.started_at));

  // Render Status Badge
  const isRunning = s.status === 'running';
  if (isRunning) {
    detailStatusEl.className = 'flex items-center gap-1.5 text-xs font-semibold rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 px-2.5 py-0.5';
    detailStatusEl.innerHTML = `<span class="relative flex h-1.5 w-1.5"><span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span><span class="relative inline-flex rounded-full h-1.5 w-1.5 bg-emerald-500"></span></span> Running`;
    killSessionBtn.classList.remove('hidden');
  } else {
    detailStatusEl.className = 'flex items-center gap-1.5 text-xs font-semibold rounded-full bg-zinc-800 text-zinc-400 border border-zinc-700 px-2.5 py-0.5';
    detailStatusEl.innerHTML = `<span class="h-1.5 w-1.5 rounded-full bg-zinc-500"></span> Completed`;
    killSessionBtn.classList.add('hidden');
  }

  // Display Task Prompts
  taskContentWrapper.textContent = s.task;
  
  // Clear Logs Viewport
  logsContainer.innerHTML = `
    <div id="logs-loader" class="flex flex-col items-center justify-center py-24 text-zinc-500 space-y-2">
      <i data-lucide="loader-2" class="h-8 w-8 animate-spin text-emerald-500"></i>
      <span class="text-xs font-medium">Connecting to stream...</span>
    </div>
  `;
  (window as any).lucide?.createIcons();

  // Connect SSE log stream
  connectSSEStream(id);
};

// Start streaming NDJSON logs for the selected session
function connectSSEStream(id: string) {
  eventSource = new EventSource(`/api/sessions/${id}/logs`);

  eventSource.onopen = () => {
    console.log(`SSE connected for session ${id}`);
    const loader = document.getElementById('logs-loader');
    if (loader) loader.remove();
  };

  eventSource.onmessage = (event) => {
    try {
      const line = event.data;
      if (!line || line.trim() === '') return;
      const data = JSON.parse(line);
      processLogEvent(data);
    } catch (err) {
      console.error('Error parsing streaming event:', err, event.data);
    }
  };

  eventSource.addEventListener('caught_up', () => {
    console.log('SSE log catchup complete');
    const loader = document.getElementById('logs-loader');
    if (loader) loader.remove();
    if (isFollowingLogs) {
      scrollToBottom();
    }
  });

  eventSource.addEventListener('end', () => {
    console.log('SSE stream reached the end of log file.');
    eventSource?.close();
    eventSource = null;
    
    // Update active badge in selected header
    detailStatusEl.className = 'flex items-center gap-1.5 text-xs font-semibold rounded-full bg-zinc-800 text-zinc-400 border border-zinc-700 px-2.5 py-0.5';
    detailStatusEl.innerHTML = `<span class="h-1.5 w-1.5 rounded-full bg-zinc-500"></span> Completed`;
    killSessionBtn.classList.add('hidden');
    
    // Trigger list reload to catch cached values
    fetchSessions();
  });

  eventSource.onerror = (err) => {
    console.error('SSE Error:', err);
    eventSource?.close();
    eventSource = null;
    
    // Check if logs are empty and show appropriate message
    if (logsContainer.children.length === 0) {
      logsContainer.innerHTML = `
        <div class="flex flex-col items-center justify-center py-24 text-zinc-500">
          <i data-lucide="slash" class="h-8 w-8 mb-2"></i>
          <span class="text-sm font-semibold">No log records yet</span>
          <span class="text-xs text-zinc-600 mt-1">Wait for the agent process to initialize its outputs.</span>
        </div>
      `;
      (window as any).lucide?.createIcons();
    }
  };
}

// Process one log event from the stream and append it to viewport
function processLogEvent(event: any) {
  const eventType = event.type;

  // 1. Turn boundary triggers
  if (eventType === 'turn_start') {
    renderState.currentTurnNum++;
    
    const turnEl = document.createElement('div');
    turnEl.className = 'border-t border-zinc-800/80 pt-6 mt-6 first:mt-0 first:border-0 first:pt-0 space-y-4';
    turnEl.innerHTML = `
      <div class="flex items-center space-x-2 text-xs font-semibold text-zinc-500 select-none">
        <span class="px-2 py-0.5 bg-zinc-900 border border-zinc-800 rounded">Turn ${renderState.currentTurnNum}</span>
        <div class="h-px bg-zinc-800/60 flex-1"></div>
      </div>
      <div class="space-y-4" id="turn-body-${renderState.currentTurnNum}"></div>
    `;
    
    logsContainer.appendChild(turnEl);
    renderState.currentTurnEl = document.getElementById(`turn-body-${renderState.currentTurnNum}`) as HTMLDivElement;
    
    // Reset message contexts
    renderState.currentAssistantMsgEl = null;
    renderState.currentAssistantTextEl = null;
    renderState.currentThinkingEl = null;
    renderState.currentThinkingTextEl = null;
    renderState.currentToolCallEl = null;
    renderState.currentToolArgsEl = null;
    
    if (isFollowingLogs) scrollToBottom();
    return;
  }

  if (eventType === 'turn_end') {
    // Sync header turn counter
    detailTurnsEl.textContent = renderState.currentTurnNum.toString();
    
    // Extract metadata values if present on turn_end
    if (event.message && event.message.usage) {
      const costTotal = event.message.usage.cost?.total;
      if (typeof costTotal === 'number' && costTotal > 0) {
        detailCostEl.textContent = `$${costTotal.toFixed(4)}`;
      }
    }
    
    if (isFollowingLogs) scrollToBottom();
    return;
  }

  // Ensure we have a turn container, create one lazily if missing (e.g., streaming started mid-way)
  if (!renderState.currentTurnEl) {
    renderState.currentTurnNum++;
    const turnEl = document.createElement('div');
    turnEl.className = 'space-y-4';
    turnEl.innerHTML = `<div class="space-y-4" id="turn-body-${renderState.currentTurnNum}"></div>`;
    logsContainer.appendChild(turnEl);
    renderState.currentTurnEl = document.getElementById(`turn-body-${renderState.currentTurnNum}`) as HTMLDivElement;
  }

  // 2. Message start
  if (eventType === 'message_start') {
    const role = event.message?.role;
    if (role === 'user') {
      // Display user message block
      const userMsg = event.message.content?.[0]?.text || '';
      if (userMsg.trim() !== '') {
        const userEl = document.createElement('div');
        userEl.className = 'bg-zinc-900/45 border border-zinc-800/60 rounded-lg p-4 max-w-4xl space-y-2';
        userEl.innerHTML = `
          <div class="flex items-center gap-1.5 text-xs text-zinc-400 select-none font-semibold">
            <i data-lucide="user" class="h-3.5 w-3.5 text-zinc-500"></i>
            <span>User</span>
          </div>
          <div class="text-zinc-200 select-text font-mono leading-relaxed whitespace-pre-wrap text-xs md:text-sm">${escapeHtml(userMsg)}</div>
        `;
        renderState.currentTurnEl.appendChild(userEl);
        (window as any).lucide?.createIcons();
      }
    } else if (role === 'assistant') {
      // Create assistant container
      createAssistantMessageBlock();
    }
    if (isFollowingLogs) scrollToBottom();
    return;
  }

  // 3. Message updates (deltas)
  if (eventType === 'message_update') {
    const update = event.assistantMessageEvent;
    if (!update) return;

    const uType = update.type;

    if (uType === 'thinking_start') {
      // Lazily create assistant block if not started
      if (!renderState.currentAssistantMsgEl) {
        createAssistantMessageBlock();
      }
      
      const thinkContainer = document.createElement('div');
      thinkContainer.className = 'border-l-2 border-zinc-700 bg-zinc-900/20 py-2.5 px-4 rounded-r my-2 space-y-1';
      thinkContainer.innerHTML = `
        <div class="flex items-center space-x-1.5 text-xs text-zinc-500 font-semibold select-none cursor-pointer hover:text-zinc-400" onclick="toggleThinkingCollapse(this)">
          <i data-lucide="lightbulb" class="h-3.5 w-3.5 text-yellow-500/80"></i>
          <span>Agent Thinking Process</span>
          <i data-lucide="chevron-down" class="h-3 w-3 shrink-0 ml-1 transition-transform"></i>
        </div>
        <div class="text-xs text-zinc-400 font-mono italic select-text whitespace-pre-wrap leading-relaxed border-t border-zinc-800/30 pt-1.5" id="thinking-content"></div>
      `;
      
      renderState.currentAssistantMsgEl!.appendChild(thinkContainer);
      renderState.currentThinkingEl = thinkContainer;
      renderState.currentThinkingTextEl = thinkContainer.querySelector('#thinking-content') as HTMLDivElement;
      
      (window as any).lucide?.createIcons();
    } 
    
    else if (uType === 'thinking_delta') {
      if (renderState.currentThinkingTextEl) {
        renderState.currentThinkingTextEl.textContent += update.delta;
      }
    } 
    
    else if (uType === 'thinking_end') {
      renderState.currentThinkingEl = null;
      renderState.currentThinkingTextEl = null;
    } 
    
    else if (uType === 'text_start') {
      if (!renderState.currentAssistantMsgEl) {
        createAssistantMessageBlock();
      }
      
      const txtEl = document.createElement('div');
      txtEl.className = 'text-zinc-200 select-text leading-relaxed whitespace-pre-wrap select-text space-y-2 text-xs md:text-sm';
      renderState.currentAssistantMsgEl!.appendChild(txtEl);
      renderState.currentAssistantTextEl = txtEl;
    } 
    
    else if (uType === 'text_delta') {
      if (!renderState.currentAssistantTextEl) {
        // Fallback if text_start didn't trigger
        if (!renderState.currentAssistantMsgEl) createAssistantMessageBlock();
        const txtEl = document.createElement('div');
        txtEl.className = 'text-zinc-200 select-text leading-relaxed whitespace-pre-wrap select-text space-y-2 text-xs md:text-sm';
        renderState.currentAssistantMsgEl!.appendChild(txtEl);
        renderState.currentAssistantTextEl = txtEl;
      }
      // Simple stream append
      renderState.currentAssistantTextEl.textContent += update.delta;
    } 
    
    else if (uType === 'text_end') {
      // Re-render text block with fancy local markdown when streaming is complete
      if (renderState.currentAssistantTextEl) {
        const raw = renderState.currentAssistantTextEl.textContent || '';
        renderState.currentAssistantTextEl.innerHTML = formatMarkdown(raw);
      }
      renderState.currentAssistantTextEl = null;
    }

    else if (uType === 'toolcall_start') {
      if (!renderState.currentAssistantMsgEl) {
        createAssistantMessageBlock();
      }

      const toolId = update.toolCallId || `tc-${Math.random().toString(16).slice(2, 10)}`;
      
      const toolEl = document.createElement('div');
      toolEl.className = 'border border-zinc-800/80 bg-zinc-950/40 rounded-md my-4 flex flex-col overflow-hidden shadow-sm';
      toolEl.innerHTML = `
        <div class="flex items-center justify-between px-4 py-2.5 bg-zinc-900 border-b border-zinc-800 select-none">
          <div class="flex items-center space-x-2 text-xs font-semibold text-zinc-300">
            <i data-lucide="terminal" class="h-4 w-4 text-emerald-400"></i>
            <span>Tool Call:</span>
            <span class="font-mono text-emerald-400 font-bold" id="tool-name-header">pending</span>
          </div>
          <span class="text-[10px] text-zinc-500 font-mono">ID: ${toolId.substring(0, 10)}...</span>
        </div>
        <div class="p-3.5">
          <pre class="text-xs text-zinc-300 font-mono overflow-x-auto whitespace-pre-wrap break-all leading-relaxed p-2.5 bg-zinc-950 rounded border border-zinc-900 shadow-inner" id="tool-args-pre"></pre>
        </div>
        <div id="tool-result-container" class="hidden border-t border-zinc-900 bg-zinc-900/10 p-4">
          <!-- Result loaded here -->
        </div>
      `;

      renderState.currentAssistantMsgEl!.appendChild(toolEl);
      renderState.currentToolCallEl = toolEl;
      renderState.currentToolArgsEl = toolEl.querySelector('#tool-args-pre') as HTMLPreElement;
      renderState.currentToolArgsRaw = '';

      renderState.toolCallMap.set(toolId, {
        container: toolEl,
        header: toolEl.querySelector('#tool-name-header') as HTMLDivElement,
        argsPre: renderState.currentToolArgsEl,
        resultEl: toolEl.querySelector('#tool-result-container') as HTMLDivElement,
      });

      (window as any).lucide?.createIcons();
    }

    else if (uType === 'toolcall_delta') {
      if (renderState.currentToolArgsEl && update.delta) {
        renderState.currentToolArgsRaw += update.delta;
        renderState.currentToolArgsEl.textContent = renderState.currentToolArgsRaw;
      }
    }

    else if (uType === 'toolcall_end') {
      // Try to format JSON arguments nicely
      if (renderState.currentToolArgsEl) {
        try {
          const parsed = JSON.parse(renderState.currentToolArgsRaw);
          renderState.currentToolArgsEl.textContent = JSON.stringify(parsed, null, 2);
        } catch {
          // Fallback to raw if not full JSON yet
        }
      }
      renderState.currentToolCallEl = null;
      renderState.currentToolArgsEl = null;
    }

    if (isFollowingLogs) scrollToBottom();
    return;
  }

  // 4. Tool Execution Updates
  if (eventType === 'tool_execution_start') {
    const mapItem = renderState.toolCallMap.get(event.toolCallId);
    if (mapItem) {
      mapItem.header.textContent = event.toolName;
      // Show args as structured if streaming was skipped/not loaded
      if (event.args && (!mapItem.argsPre.textContent || mapItem.argsPre.textContent === '')) {
        mapItem.argsPre.textContent = JSON.stringify(event.args, null, 2);
      }
    }
    return;
  }

  if (eventType === 'tool_execution_end') {
    const mapItem = renderState.toolCallMap.get(event.toolCallId);
    if (mapItem && mapItem.resultEl) {
      mapItem.resultEl.classList.remove('hidden');
      
      const isError = event.isError;
      const resultText = event.result?.content?.[0]?.text || '';
      const badgeClass = isError 
        ? 'text-red-400 bg-red-950/20 border-red-900/30' 
        : 'text-zinc-400 bg-zinc-900/40 border-zinc-800/80';
      const indicatorText = isError ? 'Tool Error Output' : 'Execution Output';
      const icon = isError ? 'alert-triangle' : 'check-circle-2';
      const iconClass = isError ? 'text-red-400' : 'text-zinc-500';

      mapItem.resultEl.innerHTML = `
        <div class="flex flex-col space-y-2">
          <div class="flex items-center justify-between cursor-pointer" onclick="toggleResultCollapse(this)">
            <div class="flex items-center space-x-2 text-xs font-semibold text-zinc-400">
              <i data-lucide="${icon}" class="h-3.5 w-3.5 ${iconClass}"></i>
              <span>${indicatorText}</span>
            </div>
            <div class="flex items-center space-x-2 text-[10px] text-zinc-500 font-semibold select-none">
              <span class="px-1.5 py-0.5 rounded border ${badgeClass}">${isError ? 'FAIL' : 'OK'}</span>
              <span class="text-[10px]">collapse</span>
              <i data-lucide="chevron-up" class="h-3 w-3 shrink-0 transition-transform"></i>
            </div>
          </div>
          <div class="result-body mt-2">
            <pre class="text-xs text-zinc-300 font-mono overflow-x-auto whitespace-pre-wrap break-all leading-relaxed p-3 bg-zinc-950 rounded border border-zinc-900 shadow-inner max-h-96 select-text overflow-y-auto">${escapeHtml(resultText || '[no output]')}</pre>
          </div>
        </div>
      `;

      if (isError) {
        mapItem.container.classList.add('border-red-950', 'bg-red-950/5');
      }

      (window as any).lucide?.createIcons();
    }
    if (isFollowingLogs) scrollToBottom();
    return;
  }
}

// Helper to construct a new Assistant bubble
function createAssistantMessageBlock() {
  const assEl = document.createElement('div');
  assEl.className = 'bg-zinc-900/10 border border-zinc-850 rounded-lg p-5 max-w-4xl space-y-3';
  assEl.innerHTML = `
    <div class="flex items-center gap-1.5 text-xs text-zinc-400 select-none font-semibold">
      <i data-lucide="bot" class="h-3.5 w-3.5 text-emerald-400"></i>
      <span>Agent</span>
    </div>
    <div class="space-y-4" id="assistant-message-body"></div>
  `;
  renderState.currentTurnEl!.appendChild(assEl);
  renderState.currentAssistantMsgEl = assEl.querySelector('#assistant-message-body') as HTMLDivElement;
  (window as any).lucide?.createIcons();
}

// Collapsible helper: Agent Thinking toggle
(window as any).toggleThinkingCollapse = function(el: HTMLDivElement) {
  const content = el.nextElementSibling as HTMLDivElement;
  const chevron = el.querySelector('[data-lucide="chevron-down"], [data-lucide="chevron-up"]') as HTMLElement;
  
  if (content.classList.contains('hidden')) {
    content.classList.remove('hidden');
    el.classList.remove('pb-1');
    chevron.style.transform = 'rotate(0deg)';
  } else {
    content.classList.add('hidden');
    el.classList.add('pb-1');
    chevron.style.transform = 'rotate(-90deg)';
  }
};

// Collapsible helper: Tool execution result collapse toggle
(window as any).toggleResultCollapse = function(el: HTMLDivElement) {
  const container = el.nextElementSibling as HTMLDivElement;
  const chevron = el.querySelector('[data-lucide="chevron-up"], [data-lucide="chevron-down"]') as HTMLElement;

  if (container.classList.contains('hidden')) {
    container.classList.remove('hidden');
    chevron.style.transform = 'rotate(0deg)';
  } else {
    container.classList.add('hidden');
    chevron.style.transform = 'rotate(-90deg)';
  }
};

// Action: Collapse/expand the global Prompt details block
function toggleTaskCollapse() {
  isTaskCollapsed = !isTaskCollapsed;
  if (isTaskCollapsed) {
    taskContentWrapper.classList.add('hidden');
    taskChevron.style.transform = 'rotate(180deg)';
  } else {
    taskContentWrapper.classList.remove('hidden');
    taskChevron.style.transform = 'rotate(0deg)';
  }
}

// Scroll utils
function scrollToBottom() {
  logsContainer.scrollTop = logsContainer.scrollHeight;
}

function handleLogsScroll() {
  const threshold = 150; // pixels from bottom
  const diff = logsContainer.scrollHeight - logsContainer.clientHeight - logsContainer.scrollTop;
  
  // If user scrolls up, disable auto-follow. If they are close to the bottom, enable it.
  if (diff > threshold) {
    isFollowingLogs = false;
  } else {
    isFollowingLogs = true;
  }
  updateFollowScrollButton();
}

function enableFollowScroll() {
  isFollowingLogs = true;
  scrollToBottom();
  updateFollowScrollButton();
}

function updateFollowScrollButton() {
  if (isFollowingLogs) {
    followScrollBtn.className = 'absolute bottom-6 right-6 flex items-center space-x-1.5 rounded-full bg-emerald-500 px-4 py-2 text-xs font-bold text-zinc-950 shadow-lg shadow-emerald-500/10 transition duration-150 transform translate-y-2 opacity-0 pointer-events-none';
  } else {
    followScrollBtn.className = 'absolute bottom-6 right-6 flex items-center space-x-1.5 rounded-full bg-emerald-500 px-4 py-2 text-xs font-bold text-zinc-950 hover:bg-emerald-400 shadow-lg shadow-emerald-500/20 transition duration-150 transform translate-y-0 opacity-100 pointer-events-auto cursor-pointer animate-pulse';
  }
}

// Action: Kill the running session
async function handleKillSession() {
  if (!selectedSessionId) return;
  const s = sessions.find(sess => sess.id === selectedSessionId);
  if (!s) return;

  const confirmKill = confirm(`Are you sure you want to kill active agentctl session "${selectedSessionId}"? This will stop its executing tasks.`);
  if (!confirmKill) return;

  killSessionBtn.disabled = true;
  killSessionBtn.textContent = 'Killing...';

  try {
    const res = await fetch(`/api/sessions/${selectedSessionId}/kill`, {
      method: 'POST',
    });
    
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || 'Failed to kill session');
    }

    alert(`Successfully killed agentctl session "${selectedSessionId}"`);
    fetchSessions(); // reload list
    
    // Refresh selected view by simulating selection click
    const id = selectedSessionId;
    selectedSessionId = null;
    (window as any).selectSession(id);
    
  } catch (err: any) {
    console.error('Error killing session:', err);
    alert(`Error: ${err.message}`);
    killSessionBtn.disabled = false;
    killSessionBtn.innerHTML = `<i data-lucide="circle-stop" class="h-3.5 w-3.5"></i> <span>Kill</span>`;
    (window as any).lucide?.createIcons();
  }
}

// String Formatting Utils
function formatMarkdown(text: string): string {
  if (!text) return '';

  let html = escapeHtml(text);

  // 1. Triple backticks code block format
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, (_, lang, code) => {
    return `
      <div class="border border-zinc-800 rounded-md overflow-hidden my-3 shadow-inner">
        <div class="bg-zinc-900 px-4 py-1.5 border-b border-zinc-800 text-[10px] text-zinc-400 font-mono flex items-center justify-between select-none">
          <span>${lang || 'code'}</span>
          <span class="hover:text-zinc-200 cursor-pointer" onclick="navigator.clipboard.writeText(this.parentElement.nextElementSibling.innerText).then(() => {this.innerText='copied!';setTimeout(()=>this.innerText='copy', 1500)})">copy</span>
        </div>
        <pre class="p-3 bg-zinc-950 overflow-x-auto text-xs text-zinc-300 font-mono break-all whitespace-pre leading-relaxed select-text"><code>${code.trim()}</code></pre>
      </div>
    `;
  });

  // 2. Inline code backticks
  html = html.replace(/`([^`]+)`/g, '<code class="bg-zinc-800 text-emerald-300 font-mono px-1 py-0.5 rounded text-xs break-all">$1</code>');

  // 3. Bold format
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong class="font-bold text-white">$1</strong>');

  // 4. Bullet list lines
  const lines = html.split('\n');
  let inList = false;
  const processedLines = lines.map(line => {
    const trimmed = line.trim();
    if (trimmed.startsWith('- ') || trimmed.startsWith('* ')) {
      const content = line.replace(/^[\s]*[-*]\s+/, '');
      let output = '';
      if (!inList) {
        output += '<ul class="list-disc list-inside pl-4 space-y-1 my-1.5 text-zinc-300">';
        inList = true;
      }
      output += `<li>${content}</li>`;
      return output;
    } else {
      let output = '';
      if (inList) {
        output += '</ul>';
        inList = false;
      }
      output += line;
      return output;
    }
  });
  if (inList) {
    processedLines.push('</ul>');
  }

  html = processedLines.join('\n');

  // 5. Hard line breaks (preserving code formatting)
  // Only replace newlines that aren't inside pre blocks
  const sections = html.split(/(<pre[\s\S]*?<\/pre>|<ul[\s\S]*?<\/ul>)/);
  html = sections.map((sec) => {
    if (sec.startsWith('<pre') || sec.startsWith('<ul')) return sec;
    return sec.replace(/\n/g, '<br>');
  }).join('');

  return html;
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

function formatRelativeTime(date: Date): string {
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);

  if (diffSec < 60) return 'Just now';
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  if (diffDay === 1) return 'Yesterday';
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

function formatDuration(startDate: Date): string {
  const now = new Date();
  const diffMs = now.getTime() - startDate.getTime();
  const diffSec = Math.floor(diffMs / 1000);
  
  if (diffSec < 60) return `${diffSec}s`;
  const mins = Math.floor(diffSec / 60);
  const secs = diffSec % 60;
  if (mins < 60) return `${mins}m ${secs}s`;
  
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  return `${hours}h ${remMins}m`;
}
