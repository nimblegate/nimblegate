// Negative: static literal - no user input involved. innerHTML on a
// hardcoded string is not the XSS pattern.
document.getElementById("status").innerHTML = "<em>Loading...</em>";
