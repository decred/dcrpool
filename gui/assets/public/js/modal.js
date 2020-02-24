$("#adminModal").on('show.bs.modal', function (e) {
    $("#accountModal").modal("hide");
});
$("#accountModal").on('show.bs.modal', function (e) {
    $("#adminModal").modal("hide");
});

$("#adminModal, #accountModal").on('shown.bs.modal', function (e) {
    $(this).find('input').focus();
});

$("#admin-form").on("submit", function (e) {
    e.preventDefault();
    
    $.ajax({
        url: $(this).attr('action'),
        type: $(this).attr('method'),
        data : $(this).serialize(),
        success: function(response) {
            window.location.replace("/admin");
        },
        error: function(response) {
            $("#password-error").fadeOut(300, function(){
                if (response.status === 401) {
                    $(this).find("p").text("Incorrect password")
                } else {
                    $(this).find("p").text("An error occurred")
                }
                $(this).fadeIn(300)
            });
        },
    });
    
});
