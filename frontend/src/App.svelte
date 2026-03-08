<script>
  import { onMount } from 'svelte';
  import { api } from './lib/api.js';
  import AssetUpload from './lib/components/AssetUpload.svelte';
  import CanvasEditor from './lib/components/CanvasEditor.svelte';
  import HlsVideo from './lib/components/HlsVideo.svelte';

  const defaultProject = {
    canvas: {
      width: 1280,
      height: 720,
      backgroundColor: '#111827',
      editorBackgroundColor: '#020202',
      customCSS: ''
    },
    sources: [],
    assets: [],
    output: {
      mode: 'hls',
      frameRate: 30,
      videoBitrate: '4500k',
      audioBitrate: '160k',
      audioSourceId: '',
      additionalArgs: [],
      hls: {
        segmentDuration: 2,
        listSize: 6,
        path: 'output/hls/live.m3u8',
        publicPath: '/live/live.m3u8'
      },
      youTube: {
        rtmpUrl: 'rtmp://a.rtmp.youtube.com/live2',
        streamKey: '',
        preset: 'youtube-default',
        additionalArgs: []
      }
    }
  };

  const defaultStream = {
    running: false,
    mode: 'hls',
    command: [],
    lastError: ''
  };

  let project = structuredClone(defaultProject);
  let savedProject = structuredClone(defaultProject);
  let stream = structuredClone(defaultStream);
  let selectedSourceId = '';
  let dirty = false;
  let loading = true;
  let saving = false;
  let uploadBusy = false;
  let statusMessage = '';
  let errorMessage = '';
  let runtimeTexts = {};
  let pollTimer;
  let runtimeTextTimer;
  let persistedSourceIds = new Set();

  $: imageAssets = project.assets.filter((asset) => asset.kind === 'image');
  $: fontAssets = project.assets.filter((asset) => asset.kind === 'font');
  $: selectedSource = project.sources.find((source) => source.id === selectedSourceId) || null;
  $: sourceOptions = project.sources.filter((source) => source.kind === 'hls');
  $: fontFaceCSS = fontAssets
    .map((asset) => `@font-face{font-family:'asset-${asset.id}';src:url('${asset.url}');font-display:swap;}`)
    .join('');

  onMount(() => {
    loadState();
    refreshRuntimeTexts();
    pollTimer = window.setInterval(() => {
      refreshState();
    }, 5000);
    runtimeTextTimer = window.setInterval(() => {
      refreshRuntimeTexts();
    }, 2000);
    return () => {
      window.clearInterval(pollTimer);
      window.clearInterval(runtimeTextTimer);
    };
  });

  async function loadState() {
    loading = true;
    try {
      const response = await api.getState();
      project = normalizeProject(response.project);
      savedProject = structuredClone(project);
      stream = normalizeStream(response.stream);
      persistedSourceIds = sourceIdSet(project);
      dirty = false;
      if (!project.sources.some((source) => source.id === selectedSourceId)) {
        selectedSourceId = project.sources[0]?.id ?? '';
      }
      await refreshRuntimeTexts();
      errorMessage = '';
    } catch (error) {
      errorMessage = error.message;
    } finally {
      loading = false;
    }
  }

  async function refreshState() {
    try {
      const response = await api.getState();
      stream = normalizeStream(response.stream);
      if (!dirty) {
        project = normalizeProject(response.project);
        savedProject = structuredClone(project);
        persistedSourceIds = sourceIdSet(project);
      }
      await refreshRuntimeTexts();
    } catch {
    }
  }

  async function refreshRuntimeTexts() {
    try {
      runtimeTexts = await api.getRuntimeTexts();
    } catch {
    }
  }

  async function saveProject() {
    saving = true;
    try {
      const response = await api.saveState(project);
      project = normalizeProject(response.project);
      savedProject = structuredClone(project);
      stream = normalizeStream(response.stream);
      persistedSourceIds = sourceIdSet(project);
      dirty = false;
      await refreshRuntimeTexts();
      statusMessage = 'Project saved';
      errorMessage = '';
      return true;
    } catch (error) {
      errorMessage = error.message;
      return false;
    } finally {
      saving = false;
    }
  }

  async function addSource(kind) {
    if (kind === 'image' && imageAssets.length === 0) {
      errorMessage = 'Upload an image asset before adding an image source';
      return;
    }
    const source = defaultSource(kind, project);
    project = {
      ...project,
      sources: [...project.sources, source]
    };
    selectedSourceId = source.id;
    updateDirtyState();
    statusMessage = `${kind.toUpperCase()} source added`;
    errorMessage = '';
  }

  async function saveSource(source = selectedSource) {
    if (!source) {
      return;
    }
    try {
      const previousSavedProject = structuredClone(savedProject);
      const localProject = structuredClone(project);
      const response = persistedSourceIds.has(source.id)
        ? await api.updateSource(source)
        : await api.createSource(source);
      const persistedProject = normalizeProject(response.project);
      savedProject = structuredClone(persistedProject);
      project = mergeProjectState(localProject, previousSavedProject, persistedProject, { persistedSourceId: source.id });
      stream = normalizeStream(response.stream);
      persistedSourceIds = sourceIdSet(persistedProject);
      updateDirtyState();
      await refreshRuntimeTexts();
      statusMessage = `Saved ${source.name}`;
      errorMessage = '';
    } catch (error) {
      errorMessage = error.message;
    }
  }

  async function removeSelectedSource() {
    if (!selectedSource) {
      return;
    }
    if (!persistedSourceIds.has(selectedSource.id)) {
      const remainingSources = project.sources.filter((source) => source.id !== selectedSource.id);
      project = {
        ...project,
        sources: remainingSources
      };
      selectedSourceId = remainingSources[0]?.id ?? '';
      updateDirtyState();
      statusMessage = 'Source removed locally';
      errorMessage = '';
      return;
    }
    try {
      const previousSavedProject = structuredClone(savedProject);
      const localProject = structuredClone(project);
      const response = await api.deleteSource(selectedSource.id);
      const persistedProject = normalizeProject(response.project);
      savedProject = structuredClone(persistedProject);
      project = mergeProjectState(localProject, previousSavedProject, persistedProject, { deletedSourceId: selectedSource.id });
      stream = normalizeStream(response.stream);
      persistedSourceIds = sourceIdSet(persistedProject);
      selectedSourceId = project.sources[0]?.id ?? '';
      updateDirtyState();
      await refreshRuntimeTexts();
      statusMessage = 'Source deleted';
      errorMessage = '';
    } catch (error) {
      errorMessage = error.message;
    }
  }

  async function startStream() {
    if (dirty) {
      const saved = await saveProject();
      if (!saved) {
        return;
      }
    }
    try {
      const response = await api.startStream();
      project = normalizeProject(response.project);
      savedProject = structuredClone(project);
      stream = normalizeStream(response.stream);
      dirty = false;
      await refreshRuntimeTexts();
      statusMessage = 'Stream started';
      errorMessage = '';
    } catch (error) {
      errorMessage = error.message;
    }
  }

  async function stopStream() {
    try {
      const previousSavedProject = structuredClone(savedProject);
      const localProject = structuredClone(project);
      const response = await api.stopStream();
      const persistedProject = normalizeProject(response.project);
      savedProject = structuredClone(persistedProject);
      project = mergeProjectState(localProject, previousSavedProject, persistedProject);
      stream = normalizeStream(response.stream);
      updateDirtyState();
      await refreshRuntimeTexts();
      statusMessage = 'Stop requested';
      errorMessage = '';
    } catch (error) {
      errorMessage = error.message;
    }
  }

  async function handleUpload(event) {
    uploadBusy = true;
    try {
      const previousSavedProject = structuredClone(savedProject);
      const localProject = structuredClone(project);
      const response = await api.uploadAsset(event.detail.kind, event.detail.file);
      const persistedProject = normalizeProject(response.project);
      savedProject = structuredClone(persistedProject);
      project = mergeProjectState(localProject, previousSavedProject, persistedProject);
      stream = normalizeStream(response.stream);
      persistedSourceIds = sourceIdSet(persistedProject);
      updateDirtyState();
      await refreshRuntimeTexts();
      statusMessage = `${event.detail.kind} uploaded`;
      errorMessage = '';
    } catch (error) {
      errorMessage = error.message;
    } finally {
      uploadBusy = false;
    }
  }

  function handleCanvasChange(event) {
    replaceSource(event.detail.source);
    if (event.detail.persist) {
      saveSource(event.detail.source);
    }
  }

  function replaceSource(nextSource) {
    project = {
      ...project,
      sources: project.sources.map((source) => (source.id === nextSource.id ? nextSource : source))
    };
    updateDirtyState();
  }

  function updateProject(mutator) {
    const nextProject = structuredClone(project);
    mutator(nextProject);
    project = nextProject;
    updateDirtyState();
  }

  function updateSelectedSource(mutator) {
    if (!selectedSource) {
      return;
    }
    updateProject((draft) => {
      const source = draft.sources.find((item) => item.id === selectedSource.id);
      if (source) {
        mutator(source);
      }
    });
  }

  function updateSelectedHLSURL(value) {
    if (!selectedSource || selectedSource.kind !== 'hls') {
      return;
    }
    updateSelectedSource((source) => {
      source.hls.url = value;
      if (!persistedSourceIds.has(source.id) && value.trim() !== '') {
        source.enabled = true;
      }
    });
  }

  function normalizeProject(rawProject) {
    const normalized = structuredClone(defaultProject);
    const merged = {
      ...normalized,
      ...rawProject,
      canvas: {
        ...normalized.canvas,
        ...(rawProject?.canvas ?? {})
      },
      output: {
        ...normalized.output,
        ...(rawProject?.output ?? {}),
        hls: {
          ...normalized.output.hls,
          ...(rawProject?.output?.hls ?? {})
        },
        youTube: {
          ...normalized.output.youTube,
          ...(rawProject?.output?.youTube ?? {})
        }
      }
    };
    merged.sources = (rawProject?.sources ?? []).map((source) => ({
      enabled: true,
      styleCSS: '',
      layout: {
        x: 0,
        y: 0,
        width: 320,
        height: 180,
        radius: 0,
        opacity: 1,
        rotation: 0,
        zIndex: 0,
        ...(source.layout ?? {})
      },
      ...source,
      hls: source.hls ?? null,
      image: source.image ?? null,
      text: source.text
        ? {
            content: '',
            fontAssetId: '',
            fontSize: 42,
            color: '#ffffff',
            backgroundColor: '#111827',
            backgroundOpacity: 0.8,
            borderColor: '#000000',
            borderWidth: 0,
            lineSpacing: 0,
            remote: {
              url: '',
              refreshIntervalSeconds: 5,
              ...(source.text.remote ?? {})
            },
            ...source.text
          }
        : null
    }));
    merged.assets = rawProject?.assets ?? [];
    merged.output.additionalArgs = rawProject?.output?.additionalArgs ?? [];
    merged.output.youTube.additionalArgs = rawProject?.output?.youTube?.additionalArgs ?? [];
    return merged;
  }

  function normalizeStream(rawStream) {
    return {
      ...defaultStream,
      ...(rawStream ?? {}),
      command: rawStream?.command ?? []
    };
  }

  function defaultSource(kind, currentProject) {
    const base = {
      id: newClientID(),
      name: `${kind.toUpperCase()} Source`,
      kind,
      enabled: true,
      styleCSS: '',
      layout: {
        x: 40 + currentProject.sources.length * 18,
        y: 40 + currentProject.sources.length * 18,
        width: kind === 'text' ? 420 : 480,
        height: kind === 'text' ? 96 : 270,
        radius: 0,
        opacity: 1,
        rotation: 0,
        zIndex: currentProject.sources.length
      }
    };

    if (kind === 'hls') {
      return {
        ...base,
        hls: {
          url: ''
        }
      };
    }
    if (kind === 'image') {
      return {
        ...base,
        image: {
          assetId: imageAssets[0]?.id ?? ''
        }
      };
    }
    return {
      ...base,
      text: {
        content: 'Streaming Studio',
        fontAssetId: fontAssets[0]?.id ?? '',
        fontSize: 44,
        color: '#ffffff',
        backgroundColor: '#111827',
        backgroundOpacity: 0.8,
        borderColor: '#000000',
        borderWidth: 0,
        lineSpacing: 0,
        remote: {
          url: '',
          refreshIntervalSeconds: 5
        }
      }
    };
  }

  function argsText(values) {
    return (values ?? []).join('\n');
  }

  function newClientID() {
    if (globalThis.crypto?.randomUUID) {
      return globalThis.crypto.randomUUID();
    }
    return `src-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }

  function parseArgsText(value) {
    return value
      .split('\n')
      .map((item) => item.trim())
      .filter(Boolean);
  }

  function sourceLabel(source) {
    return `${source.name} (${source.kind})`;
  }

  function sourceIdSet(currentProject) {
    return new Set((currentProject.sources ?? []).map((source) => source.id));
  }

  function updateDirtyState() {
    dirty = !sameJSON(project, savedProject);
  }

  function sameJSON(left, right) {
    return JSON.stringify(left) === JSON.stringify(right);
  }

  function mergeProjectState(localProject, previousSavedProject, persistedProject, options = {}) {
    const merged = structuredClone(persistedProject);

    if (!sameJSON(localProject.canvas, previousSavedProject.canvas)) {
      merged.canvas = structuredClone(localProject.canvas);
    }
    if (!sameJSON(localProject.output, previousSavedProject.output)) {
      merged.output = structuredClone(localProject.output);
    }

    const localSources = new Map((localProject.sources ?? []).map((source) => [source.id, source]));
    const savedSources = new Map((previousSavedProject.sources ?? []).map((source) => [source.id, source]));
    const mergedSources = new Map((persistedProject.sources ?? []).map((source) => [source.id, structuredClone(source)]));

    for (const [sourceId, localSource] of localSources) {
      if (sourceId === options.persistedSourceId || sourceId === options.deletedSourceId) {
        continue;
      }
      const savedSource = savedSources.get(sourceId);
      const hadUnsavedChanges = !savedSource || !sameJSON(localSource, savedSource);
      if (!hadUnsavedChanges) {
        continue;
      }
      mergedSources.set(sourceId, structuredClone(localSource));
    }

    const orderedSourceIDs = [
      ...(localProject.sources ?? []).map((source) => source.id),
      ...(persistedProject.sources ?? []).map((source) => source.id)
    ];

    merged.sources = [];
    for (const sourceId of orderedSourceIDs) {
      if (sourceId === options.deletedSourceId) {
        continue;
      }
      if (!mergedSources.has(sourceId)) {
        continue;
      }
      if (merged.sources.some((source) => source.id === sourceId)) {
        continue;
      }
      merged.sources.push(mergedSources.get(sourceId));
    }

    return merged;
  }

  function streamCommand() {
    return stream.command?.length ? stream.command.join(' ') : 'ffmpeg command will appear here after start';
  }
</script>

<svelte:head>
  <style>{fontFaceCSS}</style>
</svelte:head>

<div class="app-shell">
  <header class="hero">
    <div class="hero-main">
      <h1>Streaming Studio</h1>
      <div class="hero-meta">
        <span class:live={stream.running} class="status-pill">{stream.running ? 'LIVE' : 'IDLE'}</span>
        <div class="hero-chip">
          <span class="status-label">Output Mode</span>
          <strong>{stream.mode || project.output.mode}</strong>
        </div>
        <div class="hero-chip">
          <span class="status-label">Dirty State</span>
          <strong>{dirty ? 'Unsaved changes' : 'Saved'}</strong>
        </div>
        <div class="hero-chip">
          <span class="status-label">HLS Output</span>
          <a href={project.output.hls.publicPath} target="_blank" rel="noreferrer">{project.output.hls.publicPath}</a>
        </div>
        <div class="hero-chip hero-message">
          <span class="status-label">Messages</span>
          <strong>{errorMessage || statusMessage || 'Ready'}</strong>
          {#if stream.lastError}
            <span class="error-text">FFmpeg: {stream.lastError}</span>
          {/if}
        </div>
      </div>
    </div>
    <div class="hero-actions">
      <button class="primary" disabled={saving || loading} on:click={startStream}>Start Stream</button>
      <button class="ghost" disabled={loading} on:click={stopStream}>Stop Stream</button>
      <button class="ghost" disabled={saving || loading} on:click={saveProject}>Save Project</button>
      <button class="ghost" disabled={loading} on:click={loadState}>Reload</button>
    </div>
  </header>

  <main class="workspace">
    <aside class="panel source-panel">
      <div class="panel-header">
        <div>
          <p class="panel-eyebrow">Inputs</p>
          <h2>Sources</h2>
        </div>
      </div>

      <div class="source-actions">
        <button on:click={() => addSource('hls')}>Add HLS</button>
        <button on:click={() => addSource('image')}>Add Image</button>
        <button on:click={() => addSource('text')}>Add Text</button>
      </div>

      <div class="source-list">
        {#each project.sources as source (source.id)}
          <button
            class:selected={source.id === selectedSourceId}
            class="source-item"
            on:click={() => (selectedSourceId = source.id)}
          >
            <strong>{source.name}</strong>
            <span>{source.kind.toUpperCase()}</span>
          </button>
        {/each}
      </div>

      <div class="panel-stack">
        <div>
          <p class="panel-eyebrow">Uploads</p>
          <AssetUpload label="Upload image" kind="images" accept="image/*" busy={uploadBusy} on:upload={handleUpload} />
          <AssetUpload label="Upload font" kind="fonts" accept=".ttf,.otf,.woff,.woff2" busy={uploadBusy} on:upload={handleUpload} />
        </div>

        <div>
          <p class="panel-eyebrow">Assets</p>
          <div class="asset-list">
            {#each project.assets as asset (asset.id)}
              <div class="asset-item">
                <strong>{asset.name}</strong>
                <span>{asset.kind}</span>
              </div>
            {/each}
          </div>
        </div>
      </div>
    </aside>

    <section class="stage-panel">
      <div class="panel stage-toolbar">
        <div>
          <p class="panel-eyebrow">Scene</p>
          <h2>Canvas Editor</h2>
        </div>
        <div class="stage-actions">
          <button class="ghost" on:click={saveProject}>Save Layout</button>
        </div>
      </div>

      <CanvasEditor
        {project}
        {selectedSourceId}
        {imageAssets}
        {fontAssets}
        {runtimeTexts}
        on:select={(event) => (selectedSourceId = event.detail.sourceId)}
        on:change={handleCanvasChange}
      />

      <div class="preview-grid">
        <article class="panel preview-card">
          <div class="panel-header">
            <div>
              <p class="panel-eyebrow">Program</p>
              <h2>{project.output.mode === 'hls' ? 'Rendered HLS Output' : 'Program Preview'}</h2>
            </div>
            {#if project.output.mode === 'hls'}
              <a class="inline-link" href={project.output.hls.publicPath} target="_blank" rel="noreferrer">Open manifest</a>
            {/if}
          </div>
          {#if project.output.mode === 'hls'}
            <HlsVideo className="program-player" src={project.output.hls.publicPath} title="Rendered output" controls={true} muted={true} />
          {:else}
            <div class="preview-unavailable">
              Local HLS preview is unavailable while output mode is `youtube`.
            </div>
          {/if}
        </article>

        <article class="panel console-card">
          <div class="panel-header">
            <div>
              <p class="panel-eyebrow">FFmpeg</p>
              <h2>Current Command</h2>
            </div>
          </div>
          <pre>{streamCommand()}</pre>
        </article>
      </div>
    </section>

    <aside class="panel inspector-panel">
      <div class="panel-header">
        <div>
          <p class="panel-eyebrow">Inspector</p>
          <h2>{selectedSource ? selectedSource.name : 'Project Settings'}</h2>
        </div>
        {#if selectedSource}
          <button class="danger" on:click={removeSelectedSource}>Delete</button>
        {/if}
      </div>

      <section class="form-section">
        <h3>Canvas</h3>
        <div class="field-grid two">
          <label>
            <span>Width</span>
            <input type="number" min="320" value={project.canvas.width} on:input={(event) => updateProject((draft) => (draft.canvas.width = Number(event.currentTarget.value) || 1280))} />
          </label>
          <label>
            <span>Height</span>
            <input type="number" min="240" value={project.canvas.height} on:input={(event) => updateProject((draft) => (draft.canvas.height = Number(event.currentTarget.value) || 720))} />
          </label>
          <label>
            <span>Background</span>
            <input type="color" value={project.canvas.backgroundColor} on:input={(event) => updateProject((draft) => (draft.canvas.backgroundColor = event.currentTarget.value))} />
          </label>
          <label>
            <span>Editor Background</span>
            <input type="color" value={project.canvas.editorBackgroundColor || '#020202'} on:input={(event) => updateProject((draft) => (draft.canvas.editorBackgroundColor = event.currentTarget.value))} />
          </label>
        </div>
        <label>
          <span>Canvas CSS</span>
          <textarea rows="3" value={project.canvas.customCSS} on:input={(event) => updateProject((draft) => (draft.canvas.customCSS = event.currentTarget.value))} />
        </label>
      </section>

      {#if selectedSource}
        <section class="form-section">
          <h3>Source</h3>
          <label>
            <span>Name</span>
            <input type="text" value={selectedSource.name} on:input={(event) => updateSelectedSource((source) => (source.name = event.currentTarget.value))} />
          </label>
          <div class="field-grid two">
            <label>
              <span>X</span>
              <input type="number" value={selectedSource.layout.x} on:input={(event) => updateSelectedSource((source) => (source.layout.x = Number(event.currentTarget.value) || 0))} />
            </label>
            <label>
              <span>Y</span>
              <input type="number" value={selectedSource.layout.y} on:input={(event) => updateSelectedSource((source) => (source.layout.y = Number(event.currentTarget.value) || 0))} />
            </label>
            <label>
              <span>Width</span>
              <input type="number" min="40" value={selectedSource.layout.width} on:input={(event) => updateSelectedSource((source) => (source.layout.width = Number(event.currentTarget.value) || 40))} />
            </label>
            <label>
              <span>Height</span>
              <input type="number" min="40" value={selectedSource.layout.height} on:input={(event) => updateSelectedSource((source) => (source.layout.height = Number(event.currentTarget.value) || 40))} />
            </label>
            <label>
              <span>Opacity</span>
              <input type="number" min="0" max="1" step="0.05" value={selectedSource.layout.opacity} on:input={(event) => updateSelectedSource((source) => (source.layout.opacity = Number(event.currentTarget.value) || 0))} />
            </label>
            <label>
              <span>Radius</span>
              <input type="number" min="0" value={selectedSource.layout.radius || 0} on:input={(event) => updateSelectedSource((source) => (source.layout.radius = Math.max(0, Number(event.currentTarget.value) || 0)))} />
            </label>
            <label>
              <span>Z Index</span>
              <input type="number" value={selectedSource.layout.zIndex} on:input={(event) => updateSelectedSource((source) => (source.layout.zIndex = Number(event.currentTarget.value) || 0))} />
            </label>
          </div>
          <label class="checkbox">
            <input type="checkbox" checked={selectedSource.enabled} on:change={(event) => updateSelectedSource((source) => (source.enabled = event.currentTarget.checked))} />
            <span>Enabled</span>
          </label>
          <label>
            <span>Source CSS</span>
            <textarea rows="3" value={selectedSource.styleCSS} on:input={(event) => updateSelectedSource((source) => (source.styleCSS = event.currentTarget.value))} />
          </label>

          {#if selectedSource.kind === 'hls'}
            <label>
              <span>HLS URL</span>
              <input type="url" value={selectedSource.hls?.url || ''} on:input={(event) => updateSelectedHLSURL(event.currentTarget.value)} />
            </label>
          {/if}

          {#if selectedSource.kind === 'image'}
            <label>
              <span>Image Asset</span>
              <select value={selectedSource.image?.assetId || ''} on:change={(event) => updateSelectedSource((source) => (source.image.assetId = event.currentTarget.value))}>
                <option value="">Select image</option>
                {#each imageAssets as asset (asset.id)}
                  <option value={asset.id}>{asset.name}</option>
                {/each}
              </select>
            </label>
          {/if}

          {#if selectedSource.kind === 'text'}
            <label>
              <span>Content / Fallback</span>
              <textarea rows="4" value={selectedSource.text?.content || ''} on:input={(event) => updateSelectedSource((source) => (source.text.content = event.currentTarget.value))} />
            </label>
            <div class="field-grid two">
              <label class="full">
                <span>Poll Text URL</span>
                <input type="url" placeholder="http://kirgizu:5000/api/stream/info.txt" value={selectedSource.text?.remote?.url || ''} on:input={(event) => updateSelectedSource((source) => (source.text.remote.url = event.currentTarget.value))} />
              </label>
              <label>
                <span>Poll Interval (sec)</span>
                <input type="number" min="1" value={selectedSource.text?.remote?.refreshIntervalSeconds || 5} on:input={(event) => updateSelectedSource((source) => (source.text.remote.refreshIntervalSeconds = Math.max(1, Number(event.currentTarget.value) || 5)))} />
              </label>
            </div>
            <div class="field-grid two">
              <label>
                <span>Font Size</span>
                <input type="number" min="8" value={selectedSource.text?.fontSize || 42} on:input={(event) => updateSelectedSource((source) => (source.text.fontSize = Number(event.currentTarget.value) || 42))} />
              </label>
              <label>
                <span>Font Asset</span>
                <select value={selectedSource.text?.fontAssetId || ''} on:change={(event) => updateSelectedSource((source) => (source.text.fontAssetId = event.currentTarget.value))}>
                  <option value="">Default</option>
                  {#each fontAssets as asset (asset.id)}
                    <option value={asset.id}>{asset.name}</option>
                  {/each}
                </select>
              </label>
              <label>
                <span>Color</span>
                <input type="color" value={selectedSource.text?.color || '#ffffff'} on:input={(event) => updateSelectedSource((source) => (source.text.color = event.currentTarget.value))} />
              </label>
              <label>
                <span>Background</span>
                <input type="color" value={selectedSource.text?.backgroundColor || '#111827'} on:input={(event) => updateSelectedSource((source) => (source.text.backgroundColor = event.currentTarget.value))} />
              </label>
              <label>
                <span>Background Opacity</span>
                <input type="number" min="0" max="1" step="0.05" value={selectedSource.text?.backgroundOpacity ?? 0.8} on:input={(event) => updateSelectedSource((source) => (source.text.backgroundOpacity = Math.max(0, Math.min(1, Number(event.currentTarget.value) || 0))))} />
              </label>
              <label>
                <span>Border Color</span>
                <input type="color" value={selectedSource.text?.borderColor || '#000000'} on:input={(event) => updateSelectedSource((source) => (source.text.borderColor = event.currentTarget.value))} />
              </label>
              <label>
                <span>Border Width</span>
                <input type="number" min="0" value={selectedSource.text?.borderWidth || 0} on:input={(event) => updateSelectedSource((source) => (source.text.borderWidth = Number(event.currentTarget.value) || 0))} />
              </label>
            </div>
          {/if}

          <div class="button-row">
            <button class="primary" on:click={() => saveSource(selectedSource)}>Save Source</button>
          </div>
        </section>
      {/if}

      <section class="form-section">
        <h3>Output</h3>
        <label>
          <span>Mode</span>
          <select value={project.output.mode} on:change={(event) => updateProject((draft) => (draft.output.mode = event.currentTarget.value))}>
            <option value="hls">HLS</option>
            <option value="youtube">YouTube Live</option>
          </select>
        </label>
        <div class="field-grid two">
          <label>
            <span>Frame Rate</span>
            <input type="number" min="1" value={project.output.frameRate} on:input={(event) => updateProject((draft) => (draft.output.frameRate = Number(event.currentTarget.value) || 30))} />
          </label>
          <label>
            <span>Audio Source</span>
            <select value={project.output.audioSourceId} on:change={(event) => updateProject((draft) => (draft.output.audioSourceId = event.currentTarget.value))}>
              <option value="">First enabled HLS</option>
              {#each sourceOptions as source (source.id)}
                <option value={source.id}>{sourceLabel(source)}</option>
              {/each}
            </select>
          </label>
          <label>
            <span>Video Bitrate</span>
            <input type="text" value={project.output.videoBitrate} on:input={(event) => updateProject((draft) => (draft.output.videoBitrate = event.currentTarget.value))} />
          </label>
          <label>
            <span>Audio Bitrate</span>
            <input type="text" value={project.output.audioBitrate} on:input={(event) => updateProject((draft) => (draft.output.audioBitrate = event.currentTarget.value))} />
          </label>
        </div>

        <label>
          <span>Additional FFmpeg Args</span>
          <textarea rows="4" value={argsText(project.output.additionalArgs)} on:input={(event) => updateProject((draft) => (draft.output.additionalArgs = parseArgsText(event.currentTarget.value)))} />
        </label>

        {#if project.output.mode === 'hls'}
          <div class="field-grid two">
            <label>
              <span>Segment Duration</span>
              <input type="number" min="1" value={project.output.hls.segmentDuration} on:input={(event) => updateProject((draft) => (draft.output.hls.segmentDuration = Number(event.currentTarget.value) || 2))} />
            </label>
            <label>
              <span>Playlist Size</span>
              <input type="number" min="1" value={project.output.hls.listSize} on:input={(event) => updateProject((draft) => (draft.output.hls.listSize = Number(event.currentTarget.value) || 6))} />
            </label>
            <label>
              <span>Manifest Path</span>
              <input type="text" value={project.output.hls.path} on:input={(event) => updateProject((draft) => (draft.output.hls.path = event.currentTarget.value))} />
            </label>
            <label>
              <span>Public URL</span>
              <input type="text" value={project.output.hls.publicPath} on:input={(event) => updateProject((draft) => (draft.output.hls.publicPath = event.currentTarget.value))} />
            </label>
          </div>
        {:else}
          <div class="field-grid two">
            <label>
              <span>RTMP URL</span>
              <input type="text" value={project.output.youTube.rtmpUrl} on:input={(event) => updateProject((draft) => (draft.output.youTube.rtmpUrl = event.currentTarget.value))} />
            </label>
            <label>
              <span>Preset</span>
              <select value={project.output.youTube.preset} on:change={(event) => updateProject((draft) => (draft.output.youTube.preset = event.currentTarget.value))}>
                <option value="youtube-default">youtube-default</option>
                <option value="custom">custom</option>
              </select>
            </label>
            <label class="full">
              <span>Stream Key</span>
              <input type="password" value={project.output.youTube.streamKey} on:input={(event) => updateProject((draft) => (draft.output.youTube.streamKey = event.currentTarget.value))} />
            </label>
          </div>
          <label>
            <span>YouTube Additional Args</span>
            <textarea rows="4" value={argsText(project.output.youTube.additionalArgs)} on:input={(event) => updateProject((draft) => (draft.output.youTube.additionalArgs = parseArgsText(event.currentTarget.value)))} />
          </label>
        {/if}
      </section>
    </aside>
  </main>
</div>
