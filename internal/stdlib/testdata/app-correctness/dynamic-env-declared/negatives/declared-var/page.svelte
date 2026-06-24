<script>
  import { env } from '$env/dynamic/public';
  // Negative: PUBLIC_API_URL IS declared in the paired wrangler.toml.
  const apiUrl = env.PUBLIC_API_URL;
  console.log(apiUrl);
</script>
