// Positive: classic .innerHTML = identifier pattern. Identifier holds
// user input from a previous line.
const profile = document.getElementById("profile");
const userBio = req.body.bio;
profile.innerHTML = userBio;
