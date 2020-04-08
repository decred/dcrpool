// Only show one modal at a time. When one modal is opening, close the other one.
$("#admin-modal").on('show.bs.modal', function (e) {
    $("#account-modal").modal("hide");
});
$("#account-modal").on('show.bs.modal', function (e) {
    $("#admin-modal").modal("hide");
});

// Automatically focus on the first text input when any modal is opened
$( ".modal").on('shown.bs.modal', function (e) {
    $(this).find('input').focus();
});

// Clear error messages when inputs are modified.
$("#admin-form").find("input").on("keyup", function(e) { 
    if (e.key != "Enter") hideError($("#admin-form"));
});
$("#account-form").find("input").on("keyup", function(e) { 
    if (e.key != "Enter") hideError($("#account-form"));
});

$("#admin-form").on("submit", function (e) {
    e.preventDefault();
    hideError($(this));
    $.ajax({
        url:  $(this).attr('action'),
        type: $(this).attr('method'),
        data: $(this).serialize(),
        success: function() {
            window.location.replace("/admin");
        },
        error: function(response) {
            if (response.status === 401 || response.status === 429) {
                showError($("#admin-form"), response.responseText);
            } else {
                showError($("#admin-form"), "An error occurred");
            }
        },
    });
});

$("#account-form").on("submit", function (e) {
    e.preventDefault();
    hideError($(this));
    var address = $(this).find("input[name='address']").val();
    $.ajax({
        url:  $(this).attr('action'),
        type: $(this).attr('method'),
        data: $(this).serialize(),
        success: function() {
            window.location.replace("/account?address="+address);
        },
        error: function(response) {
            if (response.status === 400 || response.status === 429) {
                showError($("#account-form"), response.responseText);
            } else {
                showError($("#account-form"), "An error occurred");
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
