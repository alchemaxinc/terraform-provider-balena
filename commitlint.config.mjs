export default {
  extends: ["@commitlint/config-conventional"],

  // The Copilot cloud agent always opens its branch with this commit and cannot
  // be told not to. Pull requests are squash merged, so it never reaches main.
  ignores: [(message) => message.split("\n")[0].trim() === "Initial plan"],
};
