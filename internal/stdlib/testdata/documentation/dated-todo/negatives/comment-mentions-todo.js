// Function for managing items in a user's reminder list. The variable
// "todos" appears in narrative as part of the function name and parameter,
// which is normal English usage of the lowercase word - not a marker.
function manageTasks(todos) {
  return todos.filter(t => !t.done);
}
