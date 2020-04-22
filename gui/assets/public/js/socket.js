document.addEventListener("DOMContentLoaded", function () {
    ws = new WebSocket('wss://' + window.location.host + '/ws');
    ws.addEventListener('message', function (e) {
        var msg = JSON.parse(e.data);
        updateElement("pool-hash-rate", msg.poolhashrate);
        updateElement("last-work-height", msg.lastworkheight);
        updateElement("last-payment-height", msg.lastpaymentheight);
    });
});

function updateElement(elementID, value) {
    if (value === undefined) return;
    el = document.getElementById(elementID);
    if (el.innerHTML != value) {
        el.innerHTML = value;
        flashElement(el)
    }
}

function flashElement(el) {
    el.classList.add("flash");
    setTimeout(function () { el.classList.remove("flash"); }, 1000);
}
function removeElement(el) {
    el.parentNode.removeChild(el);
}
