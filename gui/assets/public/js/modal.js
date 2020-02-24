$("#adminModal").on('show.bs.modal', function (e) {
    $("#accountModal").modal("hide");
});
$("#accountModal").on('show.bs.modal', function (e) {
    $("#adminModal").modal("hide");
});

$("#adminModal, #accountModal").on('shown.bs.modal', function (e) {
    $(this).find('input').focus();
});