(function () {
  "use strict";

  var state = {
    booting: true,
    translations: {},
    view: "login", // 'login' | 'sync-setup' | 'dashboard'
    section: "dashboard", // 'dashboard' | 'authentication' | 'syncFolder' | 'logging'
    prefsOpen: true,
    prefill: { endpoint: "", username: "" },
    error: null, // errorKind string or null
    busy: false,
    busyLabel: "",
    session: null, // { endpoint, username }
    authForm: freshAuthForm(),
    syncSetup: { path: "", busy: false, error: null },
    syncFolder: { loaded: false, loading: false, path: "", busy: false, error: null, saved: false },
    logging: {
      loaded: false,
      loading: false,
      level: "",
      format: "",
      output: "",
      path: "",
      busy: false,
      error: null,
      saved: false,
    },
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

  function selectEl(attrs, options, value) {
    var node = el(
      "select",
      attrs,
      options.map(function (opt) {
        return el("option", { value: opt.value }, [opt.label]);
      })
    );
    node.value = value;
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
    } else if (state.view === "sync-setup" && state.session) {
      r.appendChild(renderSyncSetup());
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
      state.error = null;
      if (result.needsSyncSetup) {
        state.view = "sync-setup";
        state.syncSetup = { path: "", busy: false, error: null };
      } else {
        state.view = "dashboard";
        state.section = "dashboard";
      }
    } else {
      state.error = (result && result.errorKind) || "unknown";
      state.prefill = {
        endpoint: (result && result.endpoint) || state.prefill.endpoint,
        username: (result && result.username) || state.prefill.username,
      };
    }
    render();
  }

  // ---------- First-login configuration wizard (sync folder) ----------

  function renderSyncSetup() {
    var errText = errorMessage(state.syncSetup.error);
    var hasPath = !!state.syncSetup.path;

    return el("div", { class: "login-screen" }, [
      el("div", { class: "login-card" }, [
        el("h1", {}, [t("syncSetup.heading")]),
        el("p", { class: "note" }, [t("syncSetup.body")]),
        errText ? el("div", { class: "error-banner" }, [errText]) : null,
        el("div", { class: "folder-picker" }, [
          el("div", { class: "folder-path" + (hasPath ? "" : " placeholder") }, [
            state.syncSetup.path || t("syncSetup.noneChosen"),
          ]),
          el(
            "button",
            {
              class: "btn-secondary",
              type: "button",
              disabled: state.syncSetup.busy,
              onclick: pickSyncSetupFolder,
            },
            [t("syncSetup.browseButton")]
          ),
        ]),
        el(
          "button",
          {
            class: "btn-primary",
            type: "button",
            disabled: state.syncSetup.busy || !hasPath,
            onclick: completeSyncSetup,
          },
          [state.syncSetup.busy ? t("syncSetup.saving") : t("syncSetup.continueButton")]
        ),
      ]),
    ]);
  }

  function pickSyncSetupFolder() {
    window.go.main.App.ChooseSyncFolder()
      .then(function (path) {
        if (path) {
          state.syncSetup.path = path;
          state.syncSetup.error = null;
          render();
        }
      })
      .catch(function (err) {
        state.syncSetup.error = "generic";
        render();
        console.error(err);
      });
  }

  function completeSyncSetup() {
    if (!state.syncSetup.path) return;
    state.syncSetup.busy = true;
    state.syncSetup.error = null;
    render();
    window.go.main.App.SetSyncFolder(state.syncSetup.path)
      .then(function () {
        var path = state.syncSetup.path;
        state.syncSetup.busy = false;
        state.view = "dashboard";
        state.section = "dashboard";
        // Prime the preferences panel so it doesn't need a reload.
        state.syncFolder = { loaded: true, loading: false, path: path, busy: false, error: null, saved: false };
        render();
      })
      .catch(function (err) {
        state.syncSetup.busy = false;
        state.syncSetup.error = "generic";
        render();
        console.error(err);
      });
  }

  // ---------- Dashboard shell ----------

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
        navItem("syncFolder", t("menu.syncFolder"), state.section === "syncFolder", function () {
          state.section = "syncFolder";
          render();
        }),
        navItem("logging", t("menu.logging"), state.section === "logging", function () {
          state.section = "logging";
          render();
        }),
      ]),
    ];

    return el("div", { class: "sidebar" }, [el("div", { class: "sidebar-title" }, [t("app.title")])].concat(items));
  }

  function renderContent() {
    if (state.section === "authentication") {
      return renderAuthenticationPanel();
    }
    if (state.section === "syncFolder") {
      return renderSyncFolderPanel();
    }
    if (state.section === "logging") {
      return renderLoggingPanel();
    }
    return renderDashboardHome();
  }

  function renderDashboardHome() {
    return el("div", { class: "content" }, [
      el("h2", {}, [t("dashboard.welcomeTitle")]),
      el("p", {}, [t("dashboard.welcomeBody")]),
    ]);
  }

  // freshAuthForm resets the Authentication preferences panel's local
  // status (test/save progress and result). "baseline" (the values loaded
  // when the panel was entered) is filled in lazily by
  // renderAuthenticationPanel from the live session, since it isn't known
  // until then.
  function freshAuthForm() {
    return {
      initialized: false,
      baseline: null,
      testing: false,
      saving: false,
      testPassed: false,
      testedSnapshot: null,
    };
  }

  function renderAuthenticationPanel() {
    var session = state.session || { endpoint: "", username: "" };
    var af = state.authForm;
    if (!af.initialized) {
      af.initialized = true;
      af.baseline = { endpoint: session.endpoint, username: session.username, password: "" };
    }

    var endpointInput = el("input", { id: "af-endpoint", type: "text", value: af.baseline.endpoint });
    var usernameInput = el("input", { id: "af-username", type: "text", value: af.baseline.username });
    var passwordInput = el("input", {
      id: "af-password",
      type: "password",
      placeholder: t("auth.passwordPlaceholder"),
      value: "",
    });
    var messageBox = el("div", {}, []);
    var testBtn = el("button", { class: "btn-secondary", type: "button", disabled: true }, [t("auth.testButton")]);
    var saveBtn = el("button", { class: "btn-primary", type: "button", disabled: true }, [t("auth.saveButton")]);

    function currentValues() {
      return { endpoint: endpointInput.value, username: usernameInput.value, password: passwordInput.value };
    }

    function isDirty() {
      var v = currentValues();
      return (
        v.endpoint !== af.baseline.endpoint || v.username !== af.baseline.username || v.password !== af.baseline.password
      );
    }

    function matchesTestedSnapshot() {
      if (!af.testedSnapshot) return false;
      var v = currentValues();
      return (
        v.endpoint === af.testedSnapshot.endpoint &&
        v.username === af.testedSnapshot.username &&
        v.password === af.testedSnapshot.password
      );
    }

    function setMessage(kind, text) {
      messageBox.className = kind || "";
      messageBox.textContent = text || "";
    }

    function syncButtons() {
      var busy = af.testing || af.saving;
      endpointInput.disabled = busy;
      usernameInput.disabled = busy;
      passwordInput.disabled = busy;
      testBtn.disabled = busy || !isDirty();
      // Save only ever unlocks once Test has succeeded for exactly the
      // values currently in the form: any edit invalidates a prior pass.
      saveBtn.disabled = busy || !(af.testPassed && matchesTestedSnapshot());
    }

    function onFieldInput() {
      // A stale test result (pass or fail) no longer describes what's in
      // the form once it no longer matches what was tested - clear it
      // rather than mutate `state` (which would force a full re-render and
      // drop focus mid-keystroke).
      if (af.testedSnapshot && !matchesTestedSnapshot()) {
        af.testPassed = false;
        af.testedSnapshot = null;
        setMessage(null, null);
      }
      syncButtons();
    }
    [endpointInput, usernameInput, passwordInput].forEach(function (input) {
      input.addEventListener("input", onFieldInput);
    });

    testBtn.addEventListener("click", function () {
      var v = currentValues();
      af.testing = true;
      syncButtons();
      setMessage("status-line", t("auth.testing"));
      window.go.main.App.TestLogin(v.endpoint, v.username, v.password)
        .then(function (result) {
          af.testing = false;
          if (result && result.success) {
            af.testPassed = true;
            af.testedSnapshot = v;
            setMessage("success-banner", t("auth.testSuccess"));
          } else {
            af.testPassed = false;
            af.testedSnapshot = null;
            setMessage("error-banner", errorMessage((result && result.errorKind) || "unknown"));
          }
          syncButtons();
        })
        .catch(function (err) {
          af.testing = false;
          af.testPassed = false;
          af.testedSnapshot = null;
          setMessage("error-banner", errorMessage("generic"));
          syncButtons();
          console.error(err);
        });
    });

    saveBtn.addEventListener("click", function () {
      if (!(af.testPassed && matchesTestedSnapshot())) return;
      var v = currentValues();
      af.saving = true;
      syncButtons();
      setMessage("status-line", t("auth.saving"));
      window.go.main.App.Login(v.endpoint, v.username, v.password)
        .then(function (result) {
          af.saving = false;
          if (result && result.success) {
            state.session = { endpoint: result.endpoint, username: result.username };
            state.authForm = freshAuthForm();
            render();
          } else {
            af.testPassed = false;
            af.testedSnapshot = null;
            setMessage("error-banner", errorMessage((result && result.errorKind) || "unknown"));
            syncButtons();
          }
        })
        .catch(function (err) {
          af.saving = false;
          setMessage("error-banner", errorMessage("generic"));
          syncButtons();
          console.error(err);
        });
    });

    syncButtons();

    return el("div", { class: "content" }, [
      el("h2", {}, [t("auth.panelTitle")]),
      el("div", { class: "panel" }, [
        el("div", { class: "field" }, [el("label", {}, [t("auth.serverLabel")]), endpointInput]),
        el("div", { class: "field" }, [el("label", {}, [t("auth.usernameLabel")]), usernameInput]),
        el("div", { class: "field" }, [el("label", {}, [t("auth.passwordLabel")]), passwordInput]),
        messageBox,
        el("div", { class: "panel-actions" }, [testBtn, saveBtn]),
      ]),
      el("div", { class: "panel" }, [
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

  // ---------- Preferences: sync folder ----------

  function loadSyncFolderPanel() {
    if (state.syncFolder.loaded || state.syncFolder.loading) return;
    state.syncFolder.loading = true;
    window.go.main.App.GetSyncFolder()
      .then(function (settings) {
        state.syncFolder.loaded = true;
        state.syncFolder.loading = false;
        state.syncFolder.path = (settings && settings.path) || "";
        render();
      })
      .catch(function (err) {
        state.syncFolder.loading = false;
        state.syncFolder.error = "generic";
        render();
        console.error(err);
      });
  }

  function renderSyncFolderPanel() {
    loadSyncFolderPanel();
    var sf = state.syncFolder;

    if (sf.loading && !sf.loaded) {
      return el("div", { class: "content" }, [
        el("h2", {}, [t("syncFolder.panelTitle")]),
        el("div", { class: "status-line" }, [t("common.loading")]),
      ]);
    }

    var errText = errorMessage(sf.error);
    return el("div", { class: "content" }, [
      el("h2", {}, [t("syncFolder.panelTitle")]),
      el("div", { class: "panel" }, [
        errText ? el("div", { class: "error-banner" }, [errText]) : null,
        el("div", { class: "kv-row" }, [
          el("span", { class: "k" }, [t("syncFolder.currentPath")]),
          el("span", { class: "v" }, [sf.path || t("syncSetup.noneChosen")]),
        ]),
        sf.saved ? el("div", { class: "success-banner" }, [t("syncFolder.savedNote")]) : null,
        el("div", { class: "panel-actions" }, [
          el(
            "button",
            {
              class: "btn-secondary",
              type: "button",
              disabled: sf.busy,
              onclick: function () {
                pickAndSaveSyncFolder();
              },
            },
            [sf.busy ? t("syncFolder.saving") : t("syncFolder.changeButton")]
          ),
        ]),
      ]),
    ]);
  }

  function pickAndSaveSyncFolder() {
    state.syncFolder.busy = true;
    state.syncFolder.error = null;
    state.syncFolder.saved = false;
    render();
    window.go.main.App.ChooseSyncFolder()
      .then(function (path) {
        if (!path) {
          // User cancelled the dialog.
          state.syncFolder.busy = false;
          render();
          return;
        }
        return window.go.main.App.SetSyncFolder(path).then(function () {
          state.syncFolder.busy = false;
          state.syncFolder.path = path;
          state.syncFolder.saved = true;
          render();
        });
      })
      .catch(function (err) {
        state.syncFolder.busy = false;
        state.syncFolder.error = "generic";
        render();
        console.error(err);
      });
  }

  // ---------- Preferences: logging ----------

  var LOG_LEVELS = ["trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled"];
  var LOG_FORMATS = ["console", "json"];
  var LOG_OUTPUTS = ["console", "file", "both"];

  function loadLoggingPanel() {
    if (state.logging.loaded || state.logging.loading) return;
    state.logging.loading = true;
    window.go.main.App.GetLoggingConfig()
      .then(function (cfg) {
        state.logging.loaded = true;
        state.logging.loading = false;
        state.logging.level = (cfg && cfg.level) || "info";
        state.logging.format = (cfg && cfg.format) || "console";
        state.logging.output = (cfg && cfg.output) || "console";
        state.logging.path = (cfg && cfg.path) || "";
        render();
      })
      .catch(function (err) {
        state.logging.loading = false;
        state.logging.error = "generic";
        render();
        console.error(err);
      });
  }

  function renderLoggingPanel() {
    loadLoggingPanel();
    var lg = state.logging;

    if (lg.loading && !lg.loaded) {
      return el("div", { class: "content" }, [
        el("h2", {}, [t("logging.panelTitle")]),
        el("div", { class: "status-line" }, [t("common.loading")]),
      ]);
    }

    var errText = errorMessage(lg.error);

    var levelSelect = selectEl(
      { id: "lg-level", disabled: lg.busy },
      LOG_LEVELS.map(function (l) {
        return { value: l, label: t("logging.level." + l) };
      }),
      lg.level
    );
    var formatSelect = selectEl(
      { id: "lg-format", disabled: lg.busy },
      LOG_FORMATS.map(function (f) {
        return { value: f, label: t("logging.format." + f) };
      }),
      lg.format
    );
    var outputSelect = selectEl(
      { id: "lg-output", disabled: lg.busy },
      LOG_OUTPUTS.map(function (o) {
        return { value: o, label: t("logging.output." + o) };
      }),
      lg.output
    );
    var pathInput = el("input", {
      id: "lg-path",
      type: "text",
      placeholder: t("logging.pathPlaceholder"),
      value: lg.path,
      disabled: lg.busy,
    });
    var browseLogBtn = el(
      "button",
      {
        class: "btn-secondary",
        type: "button",
        disabled: lg.busy,
        onclick: function () {
          browseLogFolder(browseLogBtn, pathInput);
        },
      },
      [t("logging.browseButton")]
    );

    var form = el(
      "form",
      {
        onsubmit: function (ev) {
          ev.preventDefault();
          saveLoggingConfig(levelSelect.value, formatSelect.value, outputSelect.value, pathInput.value);
        },
      },
      [
        el("div", { class: "field" }, [el("label", {}, [t("logging.levelLabel")]), levelSelect]),
        el("div", { class: "field" }, [el("label", {}, [t("logging.formatLabel")]), formatSelect]),
        el("div", { class: "field" }, [el("label", {}, [t("logging.outputLabel")]), outputSelect]),
        el("div", { class: "field" }, [
          el("label", {}, [t("logging.pathLabel")]),
          el("div", { class: "path-picker" }, [pathInput, browseLogBtn]),
        ]),
        errText ? el("div", { class: "error-banner" }, [errText]) : null,
        lg.saved ? el("div", { class: "success-banner" }, [t("logging.savedNote")]) : null,
        el("button", { class: "btn-primary", type: "submit", disabled: lg.busy }, [
          lg.busy ? t("logging.saving") : t("logging.saveButton"),
        ]),
      ]
    );

    return el("div", { class: "content" }, [el("h2", {}, [t("logging.panelTitle")]), el("div", { class: "panel" }, [form])]);
  }

  // browseLogFolder opens the native OS folder picker for the log file's
  // directory and writes the resulting path straight into pathInput. It
  // deliberately mutates the DOM node in place instead of going through
  // state+render(): a full re-render would discard any level/format/output
  // selections the user made in this form but hasn't saved yet.
  function browseLogFolder(browseBtn, pathInput) {
    browseBtn.disabled = true;
    pathInput.disabled = true;
    window.go.main.App.ChooseLogFolder(pathInput.value)
      .then(function (fullPath) {
        browseBtn.disabled = false;
        pathInput.disabled = false;
        if (fullPath) {
          pathInput.value = fullPath;
        }
      })
      .catch(function (err) {
        browseBtn.disabled = false;
        pathInput.disabled = false;
        console.error(err);
      });
  }

  function saveLoggingConfig(level, format, output, path) {
    state.logging.busy = true;
    state.logging.error = null;
    state.logging.saved = false;
    render();
    window.go.main.App.UpdateLoggingConfig(level, format, output, path)
      .then(function () {
        state.logging.busy = false;
        state.logging.level = level;
        state.logging.format = format;
        state.logging.output = output;
        state.logging.path = path;
        state.logging.saved = true;
        render();
      })
      .catch(function (err) {
        state.logging.busy = false;
        state.logging.error = "generic";
        render();
        console.error(err);
      });
  }

  function logout() {
    window.go.main.App.Logout().finally(function () {
      state.session = null;
      state.view = "login";
      state.prefill = { endpoint: "", username: "" };
      state.error = null;
      state.authForm = freshAuthForm();
      state.syncFolder = { loaded: false, loading: false, path: "", busy: false, error: null, saved: false };
      state.logging = {
        loaded: false,
        loading: false,
        level: "",
        format: "",
        output: "",
        path: "",
        busy: false,
        error: null,
        saved: false,
      };
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
