<script>
  // Negative: $env/static/public is the build-time variant — fails the
  // build if undefined, never crashes at runtime.
  import { PUBLIC_API_URL } from '$env/static/public';
  console.log("API:", PUBLIC_API_URL);
</script>
