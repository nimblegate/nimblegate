<script>
  // Negative: no env usage at all — nothing to flag.
  let count = 0;
  function increment() { count++; }
</script>
<button on:click={increment}>{count}</button>
