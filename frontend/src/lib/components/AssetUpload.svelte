<script>
  import { createEventDispatcher } from 'svelte';

  export let label = '';
  export let kind = 'images';
  export let accept = '*/*';
  export let busy = false;

  const dispatch = createEventDispatcher();

  function handleChange(event) {
    const file = event.currentTarget.files?.[0];
    if (!file) {
      return;
    }
    dispatch('upload', { kind, file });
    event.currentTarget.value = '';
  }
</script>

<label class="upload">
  <span>{label}</span>
  <input type="file" {accept} disabled={busy} on:change={handleChange} />
</label>
