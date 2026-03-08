<script>
  import { createEventDispatcher } from 'svelte';
  import HlsVideo from './HlsVideo.svelte';

  export let project;
  export let selectedSourceId = '';
  export let imageAssets = [];
  export let fontAssets = [];
  export let runtimeTexts = {};

  const dispatch = createEventDispatcher();

  let viewportWidth = 0;
  let interaction = null;

  $: canvasWidth = project?.canvas?.width || 1280;
  $: canvasHeight = project?.canvas?.height || 720;
  $: scale = viewportWidth > 0 ? Math.min(1, viewportWidth / canvasWidth) : 1;
  $: scaledHeight = canvasHeight * scale;
  $: sortedSources = [...(project?.sources ?? [])].sort((left, right) => left.layout.zIndex - right.layout.zIndex);

  function selectSource(sourceId) {
    dispatch('select', { sourceId });
  }

  function findSource(sourceId) {
    return project.sources.find((source) => source.id === sourceId);
  }

  function beginMove(event, source) {
    if (event.target?.dataset?.resizeHandle === 'true') {
      return;
    }
    event.preventDefault();
    selectSource(source.id);
    interaction = {
      sourceId: source.id,
      mode: 'move',
      startX: event.clientX,
      startY: event.clientY,
      snapshot: structuredClone(source)
    };
    window.addEventListener('pointermove', handlePointerMove);
    window.addEventListener('pointerup', handlePointerUp);
  }

  function beginResize(event, source) {
    event.preventDefault();
    event.stopPropagation();
    selectSource(source.id);
    interaction = {
      sourceId: source.id,
      mode: 'resize',
      startX: event.clientX,
      startY: event.clientY,
      snapshot: structuredClone(source)
    };
    window.addEventListener('pointermove', handlePointerMove);
    window.addEventListener('pointerup', handlePointerUp);
  }

  function handlePointerMove(event) {
    if (!interaction) {
      return;
    }

    const source = findSource(interaction.sourceId);
    if (!source) {
      return;
    }

    const deltaX = Math.round((event.clientX - interaction.startX) / scale);
    const deltaY = Math.round((event.clientY - interaction.startY) / scale);
    const nextSource = structuredClone(interaction.snapshot);

    if (interaction.mode === 'move') {
      nextSource.layout.x = clamp(interaction.snapshot.layout.x + deltaX, 0, Math.max(0, canvasWidth - nextSource.layout.width));
      nextSource.layout.y = clamp(interaction.snapshot.layout.y + deltaY, 0, Math.max(0, canvasHeight - nextSource.layout.height));
    } else {
      nextSource.layout.width = clamp(interaction.snapshot.layout.width + deltaX, 40, canvasWidth);
      nextSource.layout.height = clamp(interaction.snapshot.layout.height + deltaY, 40, canvasHeight);
    }

    dispatch('change', { source: nextSource, persist: false });
  }

  function handlePointerUp() {
    if (interaction) {
      const source = findSource(interaction.sourceId);
      if (source) {
        dispatch('change', { source: structuredClone(source), persist: true });
      }
    }
    interaction = null;
    window.removeEventListener('pointermove', handlePointerMove);
    window.removeEventListener('pointerup', handlePointerUp);
  }

  function assetById(assetId) {
    return imageAssets.find((asset) => asset.id === assetId) || null;
  }

  function fontFamily(source) {
    const asset = fontAssets.find((item) => item.id === source?.text?.fontAssetId);
    return asset ? `'asset-${asset.id}'` : `'Noto Sans JP', 'Noto Sans CJK JP', 'Hiragino Sans', sans-serif`;
  }

  function textPreviewContent(source) {
    if (runtimeTexts?.[source.id]) {
      return runtimeTexts[source.id];
    }
    if (source?.text?.content) {
      return source.text.content;
    }
    if (source?.text?.remote?.url) {
      return '[Remote text]';
    }
    return '';
  }

  function textLineHeight(source) {
    const fontSize = source?.text?.fontSize || 42;
    const lineSpacing = source?.text?.lineSpacing || 0;
    return Math.round(fontSize * 1.2 + lineSpacing);
  }

  function colorWithOpacity(color, opacity) {
    const normalized = (color || '#111827').replace('#', '');
    const expanded = normalized.length === 3 ? normalized.split('').map((value) => value + value).join('') : normalized;
    const red = Number.parseInt(expanded.slice(0, 2), 16) || 0;
    const green = Number.parseInt(expanded.slice(2, 4), 16) || 0;
    const blue = Number.parseInt(expanded.slice(4, 6), 16) || 0;
    const alpha = Math.min(Math.max(opacity ?? 0.8, 0), 1);
    return `rgba(${red}, ${green}, ${blue}, ${alpha})`;
  }

  function styleForSource(source) {
    const styles = [
      `left:${source.layout.x}px`,
      `top:${source.layout.y}px`,
      `width:${source.layout.width}px`,
      `height:${source.layout.height}px`,
      source.kind === 'text' ? 'overflow:visible' : '',
      source.kind === 'text' ? 'background:transparent' : '',
      `border-radius:${source.layout.radius ?? 0}px`,
      `opacity:${source.layout.opacity ?? 1}`,
      `z-index:${source.layout.zIndex ?? 0}`,
      source.id === selectedSourceId ? 'outline:2px solid rgba(246, 173, 85, 0.95)' : 'outline:1px solid rgba(255,255,255,0.14)',
      source.styleCSS || ''
    ];
    return styles.join(';');
  }

  function canvasStyle() {
    return [
      `width:${canvasWidth}px`,
      `height:${canvasHeight}px`,
      `background:${project?.canvas?.backgroundColor || '#111827'}`,
      project?.canvas?.customCSS || ''
    ].join(';');
  }

  function canvasViewportStyle() {
    return [`height:${scaledHeight}px`, `background:${project?.canvas?.editorBackgroundColor || '#020202'}`].join(';');
  }

  function textContainerStyle(source) {
    return [
      `font-family:${fontFamily(source)}`,
      `font-size:${source.text?.fontSize || 42}px`,
      `color:${source.text?.color || '#ffffff'}`,
      `line-height:${textLineHeight(source)}px`,
      `width:${source.layout.width}px`,
      `height:${source.layout.height}px`,
      `background:${colorWithOpacity(source.text?.backgroundColor || '#111827', source.text?.backgroundOpacity ?? 0.8)}`,
      `border-radius:${source.layout.radius ?? 0}px`
    ].join(';');
  }

  function clamp(value, min, max) {
    return Math.min(Math.max(value, min), max);
  }
</script>

<div class="canvas-shell">
  <div class="canvas-viewport" bind:clientWidth={viewportWidth} style={canvasViewportStyle()}>
    <div class="canvas-stage" style={`transform:scale(${scale});transform-origin:top left;${canvasStyle()}`}>
      {#each sortedSources as source (source.id)}
        <div
          class:selected={source.id === selectedSourceId}
          class="source-frame"
          style={styleForSource(source)}
          on:pointerdown={(event) => beginMove(event, source)}
          on:click={() => selectSource(source.id)}
        >
          {#if source.kind === 'hls' && source.hls?.url}
            <HlsVideo className="fill" src={source.hls.url} title={source.name} />
          {:else if source.kind === 'image'}
            {#if assetById(source.image?.assetId)?.url}
              <img class="fill" src={assetById(source.image?.assetId)?.url} alt={source.name} />
            {:else}
              <div class="missing">Image asset missing</div>
            {/if}
          {:else if source.kind === 'text'}
            <div class="text-fill" style={textContainerStyle(source)}>
              <span class="text-inline">
                {textPreviewContent(source)}
              </span>
            </div>
          {/if}
          <button
            type="button"
            class="resize-handle"
            data-resize-handle="true"
            aria-label={`${source.name} resize handle`}
            on:pointerdown={(event) => beginResize(event, source)}
          />
        </div>
      {/each}
    </div>
  </div>
</div>
