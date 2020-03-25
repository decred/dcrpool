$("#adminModal").on('show.bs.modal', function (e) {
    $("#accountModal").modal("hide");
});
$("#accountModal").on('show.bs.modal', function (e) {
    $("#adminModal").modal("hide");
});

$("#adminModal, #accountModal").on('shown.bs.modal', function (e) {
    $(this).find('input').focus();
});

$("#admin-form").find("input").on("keyup", function(e) { 
    if (e.key != "Enter") hideError($("#admin-form"));
});

$("#admin-form").on("submit", function (e) {
    e.preventDefault();
    hideError($(this));
    $.ajax({
        url: $(this).attr('action'),
        type: $(this).attr('method'),
        data : $(this).serialize(),
        success: function(response) {
            window.location.replace("/admin");
        },
        error: function(response) {
            if (response.status === 401) {
                showError($("#admin-form"), "Incorrect password");
            } else {
                showError($("#admin-form"), "An error occurred");
            }
        },
    });
    
});

function hideError(form) {
    form.find(".modal-input").css("border-bottom", "1px solid #E6EAED");
    form.find(".icon-warning").css("opacity", "0");
    form.find(".err-message").css("opacity", "0");
}

function showError(form, errorMsg) {
    form.find(".modal-input").css("border-bottom", "1px solid #ed6d47");
    form.find(".icon-warning").css("opacity", "1");
    form.find(".err-message").text(errorMsg).css("opacity", "1");
}
