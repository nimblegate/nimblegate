// Positive: another shape - .innerHTML assigned a variable that was
// constructed from user input via string concatenation (not a literal).
const userComment = req.body.comment;
const html = "<p>" + userComment + "</p>";
document.querySelector(".comments").innerHTML = html;
