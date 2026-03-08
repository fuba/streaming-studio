<script>
  import { onDestroy } from 'svelte';
  import Hls from 'hls.js';

  export let src = '';
  export let autoplay = true;
  export let muted = true;
  export let controls = false;
  export let poster = '';
  export let title = '';
  export let className = '';

  let videoElement;
  let hls;

  function detach() {
    if (hls) {
      hls.destroy();
      hls = null;
    }
    if (videoElement) {
      videoElement.pause();
      videoElement.removeAttribute('src');
      videoElement.load();
    }
  }

  async function attach() {
    detach();
    if (!videoElement || !src) {
      return;
    }

    if (videoElement.canPlayType('application/vnd.apple.mpegurl')) {
      videoElement.src = src;
    } else if (Hls.isSupported()) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: true,
        backBufferLength: 30
      });
      hls.loadSource(src);
      hls.attachMedia(videoElement);
    } else {
      return;
    }

    if (autoplay) {
      try {
        await videoElement.play();
      } catch {
      }
    }
  }

  $: if (videoElement && src) {
    attach();
  }

  $: if (videoElement && !src) {
    detach();
  }

  onDestroy(() => {
    detach();
  });
</script>

<video
  bind:this={videoElement}
  class={className}
  {autoplay}
  {muted}
  {controls}
  playsinline
  preload="auto"
  {poster}
  aria-label={title}
/>
