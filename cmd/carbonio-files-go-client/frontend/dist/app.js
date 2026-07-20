(function () {
  "use strict";

  var state = {
    booting: true,
    translations: {},
    view: "login", // 'login' | 'dashboard'
    section: "dashboard", // 'dashboard' | 'authentication'
    prefsOpen: true,
    prefill: { endpoint: "", username: "" },
    error: null, // errorKind string or null
    busy: false,
    busyLabel: "",
    session: null, // { endpoint, username }
  };

  function t(key) {
    return state.translations[key] || key;
  }

  function errorMessage(kind) {
    if (!kind) return null;
    return t("error." + kind) !== "error." + kind ? t("error." + kind) : t("error.unknown");
  }

  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    attrs = attrs || {};
    Object.keys(attrs).forEach(function (k) {
      var v = attrs[k];
      if (v === undefined || v === null || v === false) return;
      if (k === "class") node.className = v;
      else if (k.indexOf("on") === 0 && typeof v === "function") node.addEventListener(k.slice(2), v);
      else node.setAttribute(k, v === true ? "" : v);
    });
    (children || []).forEach(function (c) {
      if (c === null || c === undefined) return;
      node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
    });
    return node;
  }

  function root() {
    return document.getElementById("app");
  }

  function render() {
    var r = root();
    r.innerHTML = "";
    if (state.booting) {
      r.appendChild(renderBoot());
    } else if (state.view === "dashboard" && state.session) {
      r.appendChild(renderDashboard());
    } else {
      r.appendChild(renderLogin());
    }
  }

  function renderBoot() {
    return el("div", { class: "login-screen" }, [
      el("div", { class: "login-card" }, [el("div", { class: "status-line" }, [t("login.autoChecking")])]),
    ]);
  }

  function renderLogin() {
    var errText = errorMessage(state.error);

    var endpointInput = el("input", {
      id: "f-endpoint",
      type: "text",
      placeholder: t("login.endpointPlaceholder"),
      value: state.prefill.endpoint,
      disabled: state.busy,
    });
    var usernameInput = el("input", {
      id: "f-username",
      type: "text",
      placeholder: t("login.usernamePlaceholder"),
      value: state.prefill.username,
      disabled: state.busy,
    });
    var passwordInput = el("input", {
      id: "f-password",
      type: "password",
      placeholder: "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022",
      disabled: state.busy,
    });

    var form = el(
      "form",
      {
        onsubmit: function (ev) {
          ev.preventDefault();
          submitLogin(endpointInput.value, usernameInput.value, passwordInput.value);
        },
      },
      [
        el("div", { class: "field" }, [el("label", {}, [t("login.endpointLabel")]), endpointInput]),
        el("div", { class: "field" }, [el("label", {}, [t("login.usernameLabel")]), usernameInput]),
        el("div", { class: "field" }, [el("label", {}, [t("login.passwordLabel")]), passwordInput]),
        el("button", { class: "btn-primary", type: "submit", disabled: state.busy }, [
          state.busy ? t("login.signingIn") : t("login.submit"),
        ]),
      ]
    );

    return el("div", { class: "login-screen" }, [
      el("div", { class: "login-card" }, [
        el("h1", {}, [t("login.heading")]),
        errText ? el("div", { class: "error-banner" }, [errText]) : null,
        form,
        el("p", { class: "note" }, [t("login.rememberNote")]),
      ]),
    ]);
  }

  function submitLogin(endpoint, username, password) {
    state.busy = true;
    state.error = null;
    render();
    window.go.main.App.Login(endpoint, username, password)
      .then(handleLoginResult)
      .catch(function (err) {
        state.busy = false;
        state.error = "unknown";
        state.prefill = { endpoint: endpoint, username: username };
        render();
        console.error(err);
      });
  }

  function handleLoginResult(result) {
    state.busy = false;
    if (result && result.success) {
      state.session = { endpoint: result.endpoint, username: result.username };
      state.view = "dashboard";
      state.section = "dashboard";
      state.error = null;
    } else {
      state.error = (result && result.errorKind) || "unknown";
      state.prefill = {
        endpoint: (result && result.endpoint) || state.prefill.endpoint,
        username: (result && result.username) || state.prefill.username,
      };
    }
    render();
  }

  function renderDashboard() {
    return el("div", { class: "shell" }, [renderSidebar(), renderContent()]);
  }

  function navItem(key, label, active, onClick) {
    return el(
      "div",
      {
        class: "nav-item" + (active ? " active" : ""),
        onclick: onClick,
      },
      [label]
    );
  }

  function renderSidebar() {
    var items = [
      navItem("dashboard", t("menu.dashboard"), state.section === "dashboard", function () {
        state.section = "dashboard";
        render();
      }),
      el("div", { class: "nav-group-label" }, [t("menu.preferences")]),
      el("div", { class: "nav-children" }, [
        navItem("authentication", t("menu.authentication"), state.section === "authentication", function () {
          state.section = "authentication";
          render();
        }),
      ]),
    ];

    return el("div", { class: "sidebar" }, [el("div", { class: "sidebar-title" }, [t("app.title")]), ].concat(items));
  }

  function renderContent() {
    if (state.section === "authentication") {
      return renderAuthenticationPanel();
    }
    return renderDashboardHome();
  }

  function renderDashboardHome() {
    return el("div", { class: "content" }, [
      el("h2", {}, [t("dashboard.welcomeTitle")]),
      el("p", {}, [t("dashboard.welcomeBody")]),
    ]);
  }

  function renderAuthenticationPanel() {
    var session = state.session || { endpoint: "", username: "" };
    return el("div", { class: "content" }, [
      el("h2", {}, [t("auth.panelTitle")]),
      el("div", { class: "panel" }, [
        el("div", { class: "kv-row" }, [el("span", { class: "k" }, [t("auth.connectedAs")]), el("span", { class: "v" }, [session.username])]),
        el("div", { class: "kv-row" }, [el("span", { class: "k" }, [t("auth.server")]), el("span", { class: "v" }, [session.endpoint])]),
        el("div", { class: "panel-actions" }, [
          el(
            "button",
            {
              class: "btn-secondary",
              onclick: function () {
                if (window.confirm(t("auth.logoutConfirm"))) {
                  logout();
                }
              },
            },
            [t("auth.logout")]
          ),
        ]),
      ]),
    ]);
  }

  function logout() {
    window.go.main.App.Logout().finally(function () {
      state.session = null;
      state.view = "login";
      state.prefill = { endpoint: "", username: "" };
      state.error = null;
      render();
    });
  }

  function boot() {
    render();
    window.go.main.App.Init()
      .then(function (initial) {
        state.booting = false;
        state.translations = (initial && initial.translations) || {};
        document.documentElement.lang = (initial && initial.locale) || "en";
        document.title = t("app.title");

        if (initial && initial.attemptedAutoLogin && initial.autoLogin) {
          handleLoginResult(initial.autoLogin);
        } else {
          state.view = "login";
          render();
        }
      })
      .catch(function (err) {
        state.booting = false;
        state.translations = {};
        state.view = "login";
        render();
        console.error(err);
      });
  }

  window.addEventListener("DOMContentLoaded", boot);
})();
