<script>
  // Positive: $env/dynamic/public is runtime-resolved and can be
  // undefined at runtime. $env/static/public would inline at build time
  // and fail the build if the value is missing — strictly safer.
  import { PUBLIC_API_URL } from '$env/dynamic/public';
  console.log("API:", PUBLIC_API_URL);
</script>
