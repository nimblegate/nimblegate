// Negative: .textContent is the safe alternative - never interprets
// HTML, just sets text. No XSS surface.
const userBio = req.body.bio;
document.getElementById("profile").textContent = userBio;
