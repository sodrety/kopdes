import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const script = readFileSync(new URL("./rupiah-inputs.js", import.meta.url), "utf8");

function browserHarness({ group = ",", maximum = "", maximumMessage = "Too much", fieldName = "amount" } = {}) {
  const documentListeners = new Map();
  const fieldListeners = new Map();
  const attributes = new Map([["aria-errormessage", "amount-error"]]);
  const error = { textContent: "", hidden: true };
  const field = {
    name: fieldName,
    value: "",
    validationMessage: "",
    dataset: {
      rupiahInput: "",
      rupiahGroup: group,
      rupiahInvalid: "Invalid Rupiah",
      rupiahMax: maximum,
      rupiahMaxInvalid: maximumMessage
    },
    addEventListener(name, listener) { fieldListeners.set(name, listener); },
    dispatch(name) { fieldListeners.get(name)?.({ target: field }); },
    setCustomValidity(message) { this.validationMessage = message; },
    setAttribute(name, value) { attributes.set(name, value); },
    removeAttribute(name) { attributes.delete(name); },
    getAttribute(name) { return attributes.get(name) || null; },
    matches(selector) { return selector === "[data-rupiah-input]"; },
    closest(selector) { return selector === "form" ? form : null; }
  };
  const form = {
    reported: false,
    querySelectorAll(selector) { return selector === "[data-rupiah-input]" ? [field] : []; },
    reportValidity() { this.reported = true; }
  };
  const document = {
    addEventListener(name, listener) {
      const listeners = documentListeners.get(name) || [];
      listeners.push(listener);
      documentListeners.set(name, listeners);
    },
    querySelectorAll(selector) { return selector === "[data-rupiah-input]" ? [field] : []; },
    getElementById(id) { return id === "amount-error" ? error : null; },
    fire(name, event = {}) {
      for (const listener of documentListeners.get(name) || []) listener(event);
    }
  };
  const window = {};
  vm.runInNewContext(script, { document, window }, { filename: "rupiah-inputs.js" });
  document.fire("DOMContentLoaded");
  return { document, error, field, form, window, attributes };
}

for (const locale of [
  { name: "English", group: ",", formatted: "Rp 1,234,567" },
  { name: "Bahasa Indonesia", group: ".", formatted: "Rp 1.234.567" }
]) {
  for (const workflow of ["Simpanan", "Penarikan", "Angsuran"]) {
    test(`${workflow} formats ${locale.name} grouping and accepts either pasted separator`, () => {
      const browser = browserHarness({ group: locale.group });
      for (const pasted of ["1,234,567", "1.234.567"]) {
        browser.field.value = pasted;
        browser.field.dispatch("input");
        assert.equal(browser.field.value, locale.formatted);
        assert.equal(browser.field.validationMessage, "");
      }
    });

    test(`${workflow} submits normalized digits without changing the visible ${locale.name} value`, () => {
      const browser = browserHarness({ group: locale.group });
      browser.field.value = "1234567";
      browser.field.dispatch("input");
      const submit = { target: browser.form, prevented: false, preventDefault() { this.prevented = true; } };
      browser.document.fire("submit", submit);
      assert.equal(submit.prevented, false);
      assert.equal(browser.field.value, locale.formatted);

      const nativeData = new Map();
      browser.document.fire("formdata", { target: browser.form, formData: nativeData });
      assert.equal(nativeData.get("amount"), "1234567");

      const parameters = {};
      browser.document.fire("htmx:configRequest", { detail: { elt: browser.field, parameters } });
      assert.equal(parameters.amount, "1234567");
      assert.equal(browser.field.value, locale.formatted);

      browser.field.value = "1234567";
      browser.document.fire("htmx:afterRequest", { detail: { elt: browser.form } });
      assert.equal(browser.field.value, locale.formatted, "a failed HTMX response must restore grouping");
    });
  }
}

for (const scenario of [
  { workflow: "Penarikan", message: "Jumlah penarikan melebihi saldo" },
  { workflow: "Angsuran", message: "Repayment exceeds remaining balance" }
]) {
  test(`${scenario.workflow} enforces the formatted maximum at the boundary and exposes the error accessibly`, () => {
    const browser = browserHarness({ maximum: "1000000", maximumMessage: scenario.message });
    browser.field.value = "1,000,000";
    browser.field.dispatch("input");
    assert.equal(browser.field.validationMessage, "");
    assert.equal(browser.attributes.has("aria-invalid"), false);
    assert.equal(browser.error.hidden, true);

    browser.field.value = "1.000.001";
    browser.field.dispatch("input");
    assert.equal(browser.field.value, "Rp 1,000,001");
    assert.equal(browser.field.validationMessage, scenario.message);
    assert.equal(browser.attributes.get("aria-invalid"), "true");
    assert.equal(browser.error.textContent, scenario.message);
    assert.equal(browser.error.hidden, false);

    const submit = { target: browser.form, prevented: false, preventDefault() { this.prevented = true; } };
    browser.document.fire("submit", submit);
    assert.equal(submit.prevented, true);
    assert.equal(browser.form.reported, true);
  });
}

test("Simpanan invalid input exposes a live accessible error", () => {
  const browser = browserHarness();
  browser.field.value = "-1";
  browser.field.dispatch("input");
  assert.equal(browser.attributes.get("aria-invalid"), "true");
  assert.equal(browser.error.textContent, "Invalid Rupiah");
  assert.equal(browser.error.hidden, false);
});

test("Pinjaman requested_amount formats while typing and submits normalized digits", () => {
  const browser = browserHarness({ fieldName: "requested_amount", group: "." });
  browser.field.value = "1250000";
  browser.field.dispatch("input");
  assert.equal(browser.field.value, "Rp 1.250.000");
  assert.equal(browser.field.validationMessage, "");

  const parameters = {};
  browser.document.fire("htmx:configRequest", { detail: { elt: browser.field, parameters } });
  assert.equal(parameters.requested_amount, "1250000");
});

function loanPreviewHarness({ type = "regular", group = ",", amount = "", duration = "" } = {}) {
  const documentListeners = new Map();
  const formListeners = new Map();
  const typeField = { value: type };
  const amountField = { value: amount, dataset: { rupiahGroup: group } };
  const durationField = { value: duration, max: "" };
  const output = { textContent: "", hidden: true };
  const form = {
    dataset: {
      monthlyAdminFeeLabel: "Monthly admin fee",
      totalAdminFeeLabel: "Total admin fee",
      totalObligationLabel: "Total obligation",
      rupiahGroup: group
    },
    addEventListener(name, listener) { formListeners.set(name, listener); },
    dispatch(name) { formListeners.get(name)?.({ target: form }); },
    querySelector(selector) {
      if (selector === "[data-loan-type]") return typeField;
      if (selector === "[data-loan-amount]") return amountField;
      if (selector === "[data-loan-duration]") return durationField;
      if (selector === "[data-loan-fee-output]") return output;
      return null;
    },
    querySelectorAll() { return []; }
  };
  const document = {
    addEventListener(name, listener) {
      const listeners = documentListeners.get(name) || [];
      listeners.push(listener);
      documentListeners.set(name, listeners);
    },
    querySelectorAll(selector) {
      if (selector === "[data-loan-fee-preview]") return [form];
      return [];
    },
    getElementById() { return null; },
    fire(name, event = {}) {
      for (const listener of documentListeners.get(name) || []) listener(event);
    }
  };
  const window = {};
  vm.runInNewContext(script, { document, window, BigInt }, { filename: "rupiah-inputs.js" });
  document.fire("DOMContentLoaded");
  return { amountField, durationField, form, output, typeField, window };
}

test("Pinjaman preview calculates Regular tiered monthly admin fee and total obligation", () => {
  const browser = loanPreviewHarness({ type: "regular", amount: "Rp 30,000,000", duration: "24" });
  assert.equal(browser.durationField.max, "24");
  assert.equal(browser.output.hidden, false);
  assert.equal(browser.output.textContent, "Monthly admin fee: Rp 325,000 · Total admin fee: Rp 7,800,000 · Total obligation: Rp 37,800,000");
});

test("Pinjaman preview calculates one-time secondary goods fee with Bahasa grouping", () => {
  const browser = loanPreviewHarness({ type: "secondary_goods", group: ".", amount: "Rp 1.000.001", duration: "15" });
  assert.equal(browser.durationField.max, "12");
  assert.equal(browser.durationField.value, "12");
  assert.equal(browser.output.hidden, false);
  assert.equal(browser.output.textContent, "Total admin fee: Rp 200.000 · Total obligation: Rp 1.200.001");
});

test("Pinjaman preview fixes Paylater tenor to one month and rounds fractional Rupiah half up", () => {
  const browser = loanPreviewHarness({ type: "goods_purchase_paylater", amount: "Rp 1,230", duration: "9" });
  assert.equal(browser.durationField.max, "1");
  assert.equal(browser.durationField.value, "1");
  assert.equal(browser.output.hidden, false);
  assert.equal(browser.output.textContent, "Total admin fee: Rp 62 · Total obligation: Rp 1,292");
});

for (const locale of [
  { name: "English", group: ",", maximum: "Rp 9,223,372,036,854,775,807", overMaximum: "Rp 9,223,372,036,854,775,808", message: "Enter a whole Rupiah amount from Rp 1 to Rp 9,223,372,036,854,775,807" },
  { name: "Bahasa Indonesia", group: ".", maximum: "Rp 9.223.372.036.854.775.807", overMaximum: "Rp 9.223.372.036.854.775.808", message: "Masukkan jumlah Rupiah bulat dari Rp 1 sampai Rp 9.223.372.036.854.775.807" }
]) {
  test(`generic persistence boundary is accessible in ${locale.name}`, () => {
    const browser = browserHarness({ group: locale.group });
    browser.field.dataset.rupiahInvalid = locale.message;

    browser.field.value = "9.223.372.036.854.775.807";
    browser.field.dispatch("input");
    assert.equal(browser.field.value, locale.maximum);
    assert.equal(browser.field.validationMessage, "");
    assert.equal(browser.attributes.has("aria-invalid"), false);
    assert.equal(browser.error.hidden, true);

    browser.field.value = "9,223,372,036,854,775,808";
    browser.field.dispatch("input");
    assert.equal(browser.field.value, locale.overMaximum);
    assert.equal(browser.field.validationMessage, locale.message);
    assert.equal(browser.attributes.get("aria-invalid"), "true");
    assert.equal(browser.error.textContent, locale.message);
    assert.equal(browser.error.hidden, false);
  });
}
