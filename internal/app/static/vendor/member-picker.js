(function () {
  "use strict";

  function enhanceMemberPicker(root) {
    if (root.dataset.memberPickerReady) return;
    const select = root.querySelector("select");
    if (!select) return;
    root.dataset.memberPickerReady = "true";
    const members = Array.from(select.options).filter(option => option.value).map(option => ({
      value: option.value,
      label: option.textContent.trim()
    }));
    const input = document.createElement("input");
    input.id = select.id;
    select.id += "-value";
    input.type = "text";
    input.autocomplete = "off";
    input.required = select.required;
    input.placeholder = root.dataset.placeholder;
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-expanded", "false");
    const list = document.createElement("div");
    list.id = input.id + "-results";
    list.className = "member-picker-results";
    list.setAttribute("role", "listbox");
    list.setAttribute("aria-labelledby", input.id + "-label");
    list.hidden = true;
    input.setAttribute("aria-controls", list.id);
    const status = document.createElement("div");
    status.className = "member-picker-status";
    status.setAttribute("role", "status");
    status.hidden = true;
    root.append(input, list, status);
    select.hidden = true;
    select.required = false;
    input.value = members.find(member => member.value === select.value)?.label || "";
    input.setCustomValidity(select.value ? "" : root.dataset.invalid);
    let matches = [];
    let active = -1;

    function close() {
      list.hidden = true;
      status.hidden = true;
      input.setAttribute("aria-expanded", "false");
      input.removeAttribute("aria-activedescendant");
    }

    function highlight(index) {
      active = index;
      Array.from(list.children).forEach((option, position) => option.setAttribute("aria-selected", String(position === active)));
      if (active < 0) {
        input.removeAttribute("aria-activedescendant");
      } else {
        input.setAttribute("aria-activedescendant", list.children[active].id);
        list.children[active].scrollIntoView({ block: "nearest" });
      }
    }

    function choose(member) {
      select.value = member.value;
      input.value = member.label;
      input.setCustomValidity("");
      select.dispatchEvent(new Event("change", { bubbles: true }));
      close();
    }

    function show() {
      const selected = members.find(member => member.value === select.value);
      const query = selected?.label === input.value ? "" : input.value.trim().toLocaleLowerCase();
      const terms = query.split(/\s+/).filter(Boolean);
      matches = members.filter(member => terms.every(term => member.label.toLocaleLowerCase().includes(term)));
      list.replaceChildren();
      matches.forEach((member, index) => {
        const option = document.createElement("div");
        option.id = list.id + "-" + index;
        option.className = "member-picker-option";
        option.setAttribute("role", "option");
        option.setAttribute("aria-selected", "false");
        option.textContent = member.label;
        option.addEventListener("mousedown", event => event.preventDefault());
        option.addEventListener("click", () => choose(member));
        list.append(option);
      });
      active = -1;
      input.removeAttribute("aria-activedescendant");
      list.hidden = matches.length === 0;
      status.textContent = matches.length ? "" : root.dataset.empty;
      status.hidden = matches.length !== 0;
      input.setAttribute("aria-expanded", "true");
    }

    input.addEventListener("focus", show);
    input.addEventListener("click", show);
    input.addEventListener("input", () => {
      select.value = "";
      input.setCustomValidity(root.dataset.invalid);
      show();
    });
    input.addEventListener("keydown", event => {
      if (event.isComposing) return;
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        if (input.getAttribute("aria-expanded") === "false") show();
        if (matches.length) highlight(event.key === "ArrowDown" ? (active + 1) % matches.length : (active <= 0 ? matches.length - 1 : active - 1));
      } else if (event.key === "Enter" && input.getAttribute("aria-expanded") === "true") {
        event.preventDefault();
        if (active >= 0) choose(matches[active]);
      } else if (event.key === "Escape" || event.key === "Tab") {
        close();
      }
    });
    input.addEventListener("blur", close);
    select.form?.addEventListener("reset", () => {
      // Native controls reset after the reset event has finished.
      setTimeout(() => {
        input.value = members.find(member => member.value === select.value)?.label || "";
        input.setCustomValidity(select.value ? "" : root.dataset.invalid);
        close();
      }, 0);
    });
  }

  function initializeMemberPickers() {
    document.querySelectorAll("[data-member-picker]").forEach(enhanceMemberPicker);
  }
  initializeMemberPickers();
  document.addEventListener("htmx:load", initializeMemberPickers);
})();
