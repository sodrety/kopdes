(function () {
  "use strict";

  var colors = {
    "chart-simpanan": { border: "#5b7cf0", fill: "rgba(91, 124, 240, 0.18)" },
    "chart-pinjaman": { border: "#24c68a", fill: "rgba(36, 198, 138, 0.18)" },
    "chart-warning": { border: "#f59e0b", fill: "rgba(245, 158, 11, 0.18)" },
    "chart-danger": { border: "#dc2626", fill: "rgba(220, 38, 38, 0.18)" },
    "chart-line-simpanan": { border: "#5b7cf0", fill: "rgba(91, 124, 240, 0.16)" },
    "chart-line-pinjaman": { border: "#24c68a", fill: "rgba(36, 198, 138, 0.16)" },
    "chart-line-neraca": { border: "#35bfd1", fill: "rgba(53, 191, 209, 0.16)" }
  };

  var numberFormat = new Intl.NumberFormat("id-ID");
  var currencyFormat = new Intl.NumberFormat("id-ID", {
    style: "currency",
    currency: "IDR",
    maximumFractionDigits: 0
  });

  function colorFor(className) {
    return colors[className] || colors["chart-simpanan"];
  }

  function numberValue(value) {
    var number = Number(value);
    return Number.isFinite(number) ? number : 0;
  }

  function formatCurrency(value) {
    return currencyFormat.format(numberValue(value));
  }

  function formatAxis(value) {
    return numberFormat.format(numberValue(value));
  }

  function formatValue(value, format) {
    return format === "count" ? formatAxis(value) : formatCurrency(value);
  }

  function readBarData(root) {
    var items = Array.from(root.querySelectorAll("[data-chart-item]"));
    return {
      labels: items.map(function (item) { return item.dataset.label || ""; }),
      values: items.map(function (item) { return numberValue(item.dataset.value); }),
      colors: items.map(function (item) { return colorFor(item.dataset.color).border; })
    };
  }

  function readLineData(root) {
    var labels = Array.from(root.querySelectorAll("[data-chart-label]"));
    var series = Array.from(root.querySelectorAll("[data-chart-series]"));
    return {
      labels: labels.map(function (label) { return label.textContent.trim(); }),
      datasets: series.map(function (item) {
        var color = colorFor(item.dataset.color);
        return {
          label: item.dataset.label || "",
          data: (item.dataset.values || "").split(",").filter(Boolean).map(numberValue),
          borderColor: color.border,
          backgroundColor: color.fill,
          pointBackgroundColor: color.border,
          pointBorderColor: "#ffffff",
          pointBorderWidth: 2,
          pointRadius: 3,
          pointHoverRadius: 6,
          borderWidth: 2.5,
          tension: 0.35,
          fill: true
        };
      })
    };
  }

  function tooltipLabel(format) {
    return function (context) {
      var label = context.dataset && context.dataset.label ? context.dataset.label + ": " : "";
      var value = context.parsed.y !== undefined ? context.parsed.y : context.parsed;
      return label + formatValue(value, format);
    };
  }

  function optionsFor(type, format) {
    var isBar = type === "bar";
    return {
      responsive: true,
      maintainAspectRatio: false,
      resizeDelay: 100,
      animation: { duration: 350 },
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          display: !isBar,
          position: "bottom",
          labels: { usePointStyle: true, boxWidth: 8, padding: 18 }
        },
        tooltip: {
          mode: "index",
          intersect: false,
          callbacks: { label: tooltipLabel(format) }
        }
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: { color: "#6f747b", maxRotation: 0, autoSkip: true, maxTicksLimit: isBar ? 8 : 12 }
        },
        y: {
          beginAtZero: isBar,
          grid: { color: "#e7ecf2" },
          ticks: { color: "#6f747b", callback: formatAxis }
        }
      }
    };
  }

  function createChart(root) {
    if (!window.Chart || root.dataset.chartInitialized === "true") {
      return;
    }

    var canvas = root.querySelector(".report-chart-canvas");
    if (!canvas) {
      return;
    }

    var type = root.dataset.chart;
    var data;
    var config;
    if (type === "bar") {
      var bar = readBarData(root);
      data = {
        labels: bar.labels,
        datasets: [{
          data: bar.values,
          backgroundColor: bar.colors.map(function (color) { return color + "d9"; }),
          borderColor: bar.colors,
          borderWidth: 1,
          borderRadius: 6,
          borderSkipped: false,
          maxBarThickness: 48
        }]
      };
      config = { type: "bar", data: data, options: optionsFor("bar", root.dataset.chartFormat) };
    } else if (type === "line") {
      data = readLineData(root);
      config = { type: "line", data: data, options: optionsFor("line", root.dataset.chartFormat) };
    } else {
      return;
    }

    root.dataset.chartInitialized = "true";
    root._kopdesChart = new window.Chart(canvas, config);
  }

  function initializeCharts() {
    document.querySelectorAll("[data-chart]").forEach(createChart);
  }

  document.addEventListener("DOMContentLoaded", initializeCharts);
  document.addEventListener("htmx:afterSwap", initializeCharts);
}());
