(function () {
  "use strict";

  document.querySelectorAll("[data-password-visibility-toggle]").forEach(toggle => {
    const input = document.getElementById(toggle.dataset.passwordVisibilityToggle);
    if (!input) return;

    toggle.addEventListener("change", () => {
      input.type = toggle.checked ? "text" : "password";
    });
  });
})();
