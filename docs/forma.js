!(function () {
  var appUrl = "https://app.forma.ylam.ai"; // use PUBLIC_URL if you are using multi-domain setup, otherwise use WEBAPP_URL
  var workspaceId = "clgwcwp4z000lpf0hur7pzbuv";

  var t = document.createElement("script");
  t.type = "text/javascript";
  t.async = !0;
  t.src = appUrl + "/js/forma.umd.cjs";

  var e = document.getElementsByTagName("script")[0];
  e.parentNode.insertBefore(t, e);

  setTimeout(function () {
    window.forma.setup({ workspaceId: workspaceId, appUrl: appUrl });
  }, 500);
})();
